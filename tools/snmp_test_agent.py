#!/usr/bin/env python3
import warnings
warnings.filterwarnings("ignore", category=DeprecationWarning)

"""
SnmpLens Test Agent
===================
Pure-Python SNMP agent simulator with v1/v2c/v3 support and trap sending.
No external dependencies — uses only Python stdlib + pycryptodome (for AES).

Requirements:
    pip install pycryptodome     (for SNMPv3 AES privacy — optional)

Usage:
    python snmp_test_agent.py                          # Default: agent on port 1161
    python snmp_test_agent.py --port 1161
    python snmp_test_agent.py --trap-port 1162 --trap-interval 10
    python snmp_test_agent.py --no-traps

Credentials:
    v1/v2c community: "public" (ro), "private" (rw)
    v3 user: "snmplens"    auth: SHA / "authpass123"   priv: AES128 / "privpass123"  (authPriv)
    v3 user: "authonly"    auth: SHA / "authpass123"   (authNoPriv)
    v3 user: "noauthuser"  (noAuthNoPriv)
"""

import argparse
import asyncio
import hashlib
import hmac
import os
import random
import struct
import sys
import time
from datetime import datetime

# Try to import AES for SNMPv3 privacy
try:
    from Crypto.Cipher import AES as _AES
    HAS_AES = True
except ImportError:
    try:
        from Cryptodome.Cipher import AES as _AES
        HAS_AES = True
    except ImportError:
        HAS_AES = False


# ═══════════════════════════════════════════════════════════════════════════
# BER / ASN.1 Codec
# ═══════════════════════════════════════════════════════════════════════════

INTEGER = 0x02
OCTET_STRING = 0x04
NULL = 0x05
OID_TAG = 0x06
SEQUENCE = 0x30
IP_ADDRESS = 0x40
COUNTER32 = 0x41
GAUGE32 = 0x42
TIMETICKS = 0x43
COUNTER64 = 0x46
NO_SUCH_OBJECT = 0x80
NO_SUCH_INSTANCE = 0x81
END_OF_MIB_VIEW = 0x82
GET_REQUEST = 0xA0
GET_NEXT_REQUEST = 0xA1
GET_RESPONSE = 0xA2
SET_REQUEST = 0xA3
GET_BULK_REQUEST = 0xA5
TRAP_V2 = 0xA7


def encode_length(length):
    if length < 0x80:
        return bytes([length])
    elif length < 0x100:
        return bytes([0x81, length])
    elif length < 0x10000:
        return bytes([0x82, length >> 8, length & 0xFF])
    else:
        return bytes([0x83, length >> 16, (length >> 8) & 0xFF, length & 0xFF])


def encode_tlv(tag, value):
    return bytes([tag]) + encode_length(len(value)) + value


def encode_integer(value):
    if value == 0:
        return encode_tlv(INTEGER, b'\x00')
    neg = value < 0
    if neg:
        value = -value - 1
    result = []
    while value > 0:
        result.insert(0, (~value & 0xFF) if neg else (value & 0xFF))
        value >>= 8
    if not neg and result[0] & 0x80:
        result.insert(0, 0)
    elif neg and not (result[0] & 0x80):
        result.insert(0, 0xFF)
    if not result:
        result = [0xFF] if neg else [0]
    return encode_tlv(INTEGER, bytes(result))


def encode_counter64(value):
    value = value % (2**64)
    raw = []
    if value == 0:
        raw = [0]
    else:
        v = value
        while v > 0:
            raw.insert(0, v & 0xFF)
            v >>= 8
    if raw[0] & 0x80:
        raw.insert(0, 0)
    return encode_tlv(COUNTER64, bytes(raw))


def encode_string(value):
    if isinstance(value, str):
        value = value.encode()
    return encode_tlv(OCTET_STRING, value)


def encode_null():
    return encode_tlv(NULL, b'')


def encode_oid(oid_str):
    parts = [int(x) for x in oid_str.strip('.').split('.')]
    if len(parts) < 2:
        parts += [0] * (2 - len(parts))
    result = [parts[0] * 40 + parts[1]]
    for sub in parts[2:]:
        if sub < 128:
            result.append(sub)
        else:
            encoded = []
            while sub > 0:
                encoded.insert(0, sub & 0x7F)
                sub >>= 7
            for i in range(len(encoded) - 1):
                encoded[i] |= 0x80
            result.extend(encoded)
    return encode_tlv(OID_TAG, bytes(result))


def encode_ip_address(ip_str):
    parts = [int(x) for x in ip_str.split('.')]
    return encode_tlv(IP_ADDRESS, bytes(parts))


def encode_sequence(items):
    data = b''.join(items)
    return encode_tlv(SEQUENCE, data)


def encode_varbind(oid_str, value_bytes):
    return encode_sequence([encode_oid(oid_str), value_bytes])


def encode_value(tag, val):
    if tag == INTEGER:
        return encode_integer(val)
    elif tag == OCTET_STRING:
        return encode_tlv(OCTET_STRING, val if isinstance(val, bytes) else val.encode())
    elif tag == OID_TAG:
        return encode_oid(val)
    elif tag in (COUNTER32, GAUGE32, TIMETICKS):
        val = val % (2**32)
        raw = []
        if val == 0:
            raw = [0]
        else:
            v = val
            while v > 0:
                raw.insert(0, v & 0xFF)
                v >>= 8
        if raw[0] & 0x80:
            raw.insert(0, 0)
        return encode_tlv(tag, bytes(raw))
    elif tag == COUNTER64:
        return encode_counter64(val)
    elif tag == IP_ADDRESS:
        return encode_ip_address(val)
    elif tag == NULL:
        return encode_null()
    else:
        return encode_tlv(tag, val if isinstance(val, bytes) else b'')


def decode_length(data, offset):
    if data[offset] < 0x80:
        return data[offset], offset + 1
    n_bytes = data[offset] & 0x7F
    length = 0
    for i in range(n_bytes):
        length = (length << 8) | data[offset + 1 + i]
    return length, offset + 1 + n_bytes


def decode_tlv(data, offset):
    tag = data[offset]
    length, value_offset = decode_length(data, offset + 1)
    value = data[value_offset:value_offset + length]
    return tag, value, value_offset + length


def decode_integer(data):
    value = 0
    negative = data[0] & 0x80 if data else False
    for b in data:
        value = (value << 8) | b
    if negative:
        value -= (1 << (len(data) * 8))
    return value


def decode_oid(data):
    if not data:
        return ''
    parts = [data[0] // 40, data[0] % 40]
    i = 1
    while i < len(data):
        sub = 0
        while i < len(data):
            sub = (sub << 7) | (data[i] & 0x7F)
            if not (data[i] & 0x80):
                i += 1
                break
            i += 1
        parts.append(sub)
    return '.'.join(str(p) for p in parts)


def decode_varbind_list(data):
    varbinds = []
    offset = 0
    while offset < len(data):
        _, vb_data, offset = decode_tlv(data, offset)
        _, oid_raw, inner_off = decode_tlv(vb_data, 0)
        oid_str = decode_oid(oid_raw)
        val_tag, val_raw, _ = decode_tlv(vb_data, inner_off)
        varbinds.append((oid_str, val_tag, val_raw))
    return varbinds


# ═══════════════════════════════════════════════════════════════════════════
# SNMPv3 USM (User-based Security Model)
# ═══════════════════════════════════════════════════════════════════════════

ENGINE_ID = b'\x80\x00\x00\x00\x01\x03\x04\x05\x06\x07\x08\x09'  # 12 bytes
ENGINE_BOOTS = 1

# Auth: noAuthNoPriv=0x00, authNoPriv=0x01, authPriv=0x03
AUTH_NO_AUTH_NO_PRIV = 0x00
AUTH_NO_PRIV = 0x01
AUTH_PRIV = 0x03

V3_USERS = {}  # populated in main()


# Supported hash algorithms for key derivation and HMAC
HASH_ALGOS = {
    'sha1':   (hashlib.sha1,   20, 12),  # (constructor, digest_len, hmac_truncation)
    'sha224': (hashlib.sha224, 28, 16),
    'sha256': (hashlib.sha256, 32, 24),
    'sha384': (hashlib.sha384, 48, 32),
    'sha512': (hashlib.sha512, 64, 48),
}


def password_to_key(password, algo='sha1'):
    """RFC 3414 / RFC 7860 password-to-key: hash 1MB of repeated password."""
    password = password.encode() if isinstance(password, str) else password
    hash_fn = HASH_ALGOS[algo][0]
    h = hash_fn()
    pwd_len = len(password)
    buf = password * (1048576 // pwd_len + 1)
    h.update(buf[:1048576])
    return h.digest()


def localize_key(key, engine_id, algo='sha1'):
    """Localize a master key with an engineID: HASH(key + engineID + key)."""
    hash_fn = HASH_ALGOS[algo][0]
    return hash_fn(key + engine_id + key).digest()


def derive_keys(password, engine_id, algo='sha1'):
    """Derive localized key from password + engineID using specified hash."""
    master = password_to_key(password, algo)
    return localize_key(master, engine_id, algo)


def compute_auth_mac(auth_key, message, algo='sha1'):
    """Compute HMAC for SNMPv3 auth, truncated per RFC."""
    hash_fn, _, trunc_len = HASH_ALGOS[algo]
    return hmac.new(auth_key, message, hash_fn).digest()[:trunc_len]


def get_auth_param_len(algo='sha1'):
    """Return the length of auth parameters for a given algorithm."""
    return HASH_ALGOS[algo][2]


def aes_encrypt(priv_key, engine_boots, engine_time, salt_int, plaintext, key_len=16):
    """AES-CFB encryption for SNMPv3 privacy. key_len=16 for AES128, 32 for AES256."""
    if not HAS_AES:
        raise RuntimeError("AES not available")
    key = priv_key[:key_len]
    # IV = engineBoots(4) + engineTime(4) + salt(8) = 16 bytes
    iv = struct.pack('>II', engine_boots, engine_time) + struct.pack('>Q', salt_int)
    cipher = _AES.new(key, _AES.MODE_CFB, iv=iv, segment_size=128)
    return cipher.encrypt(plaintext)


def aes_decrypt(priv_key, engine_boots, engine_time, salt_bytes, ciphertext, key_len=16):
    """AES-CFB decryption for SNMPv3 privacy. key_len=16 for AES128, 32 for AES256."""
    if not HAS_AES:
        raise RuntimeError("AES not available")
    key = priv_key[:key_len]
    iv = struct.pack('>II', engine_boots, engine_time) + salt_bytes
    cipher = _AES.new(key, _AES.MODE_CFB, iv=iv, segment_size=128)
    return cipher.decrypt(ciphertext)


def decode_usm_security_params(data):
    """Decode USM security parameters from OCTET STRING content."""
    _, seq_data, _ = decode_tlv(data, 0)
    off = 0
    _, engine_id, off = decode_tlv(seq_data, off)
    _, boots_raw, off = decode_tlv(seq_data, off)
    _, time_raw, off = decode_tlv(seq_data, off)
    _, username, off = decode_tlv(seq_data, off)
    _, auth_params, off = decode_tlv(seq_data, off)
    _, priv_params, off = decode_tlv(seq_data, off)

    return {
        'engine_id': engine_id,
        'boots': decode_integer(boots_raw) if boots_raw else 0,
        'time': decode_integer(time_raw) if time_raw else 0,
        'username': username.decode('ascii', errors='replace'),
        'auth_params': auth_params,
        'priv_params': priv_params,
    }


def encode_usm_security_params(engine_id, boots, eng_time, username, auth_params=b'', priv_params=b''):
    """Encode USM security parameters as OCTET STRING."""
    inner = (
        encode_string(engine_id) +
        encode_integer(boots) +
        encode_integer(eng_time) +
        encode_string(username.encode() if isinstance(username, str) else username) +
        encode_string(auth_params) +
        encode_string(priv_params)
    )
    return encode_string(encode_sequence_raw(inner))


def encode_sequence_raw(data):
    """Encode a SEQUENCE from pre-built data bytes."""
    return encode_tlv(SEQUENCE, data)


SALT_COUNTER = random.randint(0, 2**63)


def next_salt():
    global SALT_COUNTER
    SALT_COUNTER += 1
    return SALT_COUNTER


# ═══════════════════════════════════════════════════════════════════════════
# SNMPv3 Message Handling
# ═══════════════════════════════════════════════════════════════════════════

def decode_snmpv3_message(data):
    """Decode an SNMPv3 message. Returns parsed components."""
    _, msg_data, _ = decode_tlv(data, 0)  # outer SEQUENCE
    off = 0

    # version
    _, ver_raw, off = decode_tlv(msg_data, off)
    version = decode_integer(ver_raw)

    # msgGlobalData (SEQUENCE: msgID, msgMaxSize, msgFlags, msgSecurityModel)
    _, global_data, off = decode_tlv(msg_data, off)
    g_off = 0
    _, msg_id_raw, g_off = decode_tlv(global_data, g_off)
    msg_id = decode_integer(msg_id_raw)
    _, max_size_raw, g_off = decode_tlv(global_data, g_off)
    _, flags_raw, g_off = decode_tlv(global_data, g_off)
    msg_flags = flags_raw[0] if flags_raw else 0
    _, sec_model_raw, g_off = decode_tlv(global_data, g_off)

    # msgSecurityParameters (OCTET STRING wrapping a SEQUENCE)
    _, sec_params_raw, off = decode_tlv(msg_data, off)
    usm = decode_usm_security_params(sec_params_raw)

    # The rest is msgData (SEQUENCE with contextEngineID, contextName, PDU)
    # May be encrypted (OCTET STRING) or plaintext (SEQUENCE)
    remaining = msg_data[off:]

    return {
        'version': version,
        'msg_id': msg_id,
        'msg_flags': msg_flags,
        'usm': usm,
        'raw': data,
        'remaining': remaining,
        'sec_params_raw': sec_params_raw,
    }


def verify_auth(msg_data, parsed, user_info):
    """Verify HMAC authentication on a received message."""
    auth_params = parsed['usm']['auth_params']
    algo = user_info.get('auth_algo', 'sha1')
    expected_len = get_auth_param_len(algo)
    if len(auth_params) != expected_len:
        return False

    auth_placeholder = b'\x00' * expected_len
    raw = bytearray(msg_data)
    idx = raw.find(auth_params)
    if idx < 0:
        return False
    raw[idx:idx+expected_len] = auth_placeholder
    expected = compute_auth_mac(user_info['auth_key'], bytes(raw), algo)
    return hmac.compare_digest(expected, auth_params)


def decode_scoped_pdu(data, parsed, user_info):
    """Decode the scoped PDU (decrypting if necessary). Returns (contextEngineID, contextName, pdu_tag, request_id, varbinds, non_rep, max_rep)."""
    msg_flags = parsed['msg_flags']
    remaining = parsed['remaining']

    if msg_flags & 0x02:  # priv flag
        # Encrypted: remaining is OCTET STRING containing ciphertext
        _, ciphertext, _ = decode_tlv(remaining, 0)
        priv_params = parsed['usm']['priv_params']
        key_len = user_info.get('priv_key_len', 16)
        plaintext = aes_decrypt(
            user_info['priv_key'],
            ENGINE_BOOTS,
            engine_time(),
            priv_params,
            ciphertext,
            key_len
        )
        remaining = plaintext

    # Parse SEQUENCE (scopedPDU): contextEngineID, contextName, PDU
    _, scoped, _ = decode_tlv(remaining, 0)
    s_off = 0
    _, ctx_engine_id, s_off = decode_tlv(scoped, s_off)
    _, ctx_name, s_off = decode_tlv(scoped, s_off)

    # PDU
    pdu_tag, pdu_data, _ = decode_tlv(scoped, s_off)

    pdu_off = 0
    _, reqid_raw, pdu_off = decode_tlv(pdu_data, pdu_off)
    request_id = decode_integer(reqid_raw)
    _, field2_raw, pdu_off = decode_tlv(pdu_data, pdu_off)
    _, field3_raw, pdu_off = decode_tlv(pdu_data, pdu_off)

    non_rep = decode_integer(field2_raw) if pdu_tag == GET_BULK_REQUEST else 0
    max_rep = decode_integer(field3_raw) if pdu_tag == GET_BULK_REQUEST else 0

    _, vb_list_data, _ = decode_tlv(pdu_data, pdu_off)
    varbinds = decode_varbind_list(vb_list_data)

    return ctx_engine_id, ctx_name, pdu_tag, request_id, varbinds, non_rep, max_rep


def engine_time():
    return int(time.time() - BOOT_TIME) % (2**31)


def build_v3_response(parsed, response_vbs, user_info, error_status=0, error_index=0):
    """Build an SNMPv3 response message with proper auth/priv."""
    msg_id = parsed['msg_id']
    msg_flags = parsed['msg_flags']
    username = parsed['usm']['username']

    # Build the scoped PDU
    request_id = 0
    # Re-extract request_id from the original decoding
    # (we pass it through the varbinds processing)
    # We'll use msg_id as request_id for the response PDU
    vb_seq = encode_sequence(response_vbs)
    pdu_body = encode_integer(msg_id) + encode_integer(error_status) + encode_integer(error_index) + vb_seq
    pdu = encode_tlv(GET_RESPONSE, pdu_body)

    scoped_pdu = encode_sequence([
        encode_string(ENGINE_ID),
        encode_string(b''),  # contextName
        pdu,
    ])

    # Privacy: encrypt scoped PDU
    priv_params = b''
    key_len = user_info.get('priv_key_len', 16)
    if (msg_flags & 0x02) and user_info.get('priv_key') and HAS_AES:
        salt = next_salt()
        priv_params = struct.pack('>Q', salt)
        encrypted = aes_encrypt(user_info['priv_key'], ENGINE_BOOTS, engine_time(), salt, scoped_pdu, key_len)
        msg_data_part = encode_string(encrypted)
    else:
        msg_data_part = scoped_pdu

    # Build security parameters (auth placeholder first)
    algo = user_info.get('auth_algo', 'sha1')
    auth_len = get_auth_param_len(algo) if (msg_flags & 0x01) else 0
    auth_placeholder = b'\x00' * auth_len
    sec_params = encode_usm_security_params(
        ENGINE_ID, ENGINE_BOOTS, engine_time(), username, auth_placeholder, priv_params
    )

    # msgGlobalData
    resp_flags = msg_flags & 0x03  # echo auth+priv flags, clear reportable
    global_data = encode_sequence([
        encode_integer(msg_id),
        encode_integer(65507),
        encode_string(bytes([resp_flags])),
        encode_integer(3),  # USM
    ])

    # Full message
    full_msg = encode_sequence([
        encode_integer(3),  # version
        global_data,
        sec_params,
        msg_data_part,
    ])

    # Authentication: compute HMAC and replace placeholder
    if (msg_flags & 0x01) and user_info.get('auth_key'):
        mac = compute_auth_mac(user_info['auth_key'], full_msg, algo)
        idx = full_msg.find(auth_placeholder)
        if idx >= 0:
            full_msg = full_msg[:idx] + mac + full_msg[idx+auth_len:]

    return full_msg


def build_v3_discovery_response(msg_id):
    """Build a discovery response (report) with our engineID."""
    # Unknown user report — has reportable flag
    sec_params = encode_usm_security_params(ENGINE_ID, ENGINE_BOOTS, engine_time(), '', b'', b'')
    global_data = encode_sequence([
        encode_integer(msg_id),
        encode_integer(65507),
        encode_string(b'\x00'),  # noAuthNoPriv, not reportable
        encode_integer(3),
    ])
    # Report PDU with usmStatsUnknownEngineIDs
    report_vbs = [encode_varbind("1.3.6.1.6.3.15.1.1.4.0", encode_integer(1))]
    pdu = encode_tlv(GET_RESPONSE, encode_integer(msg_id) + encode_integer(0) + encode_integer(0) + encode_sequence(report_vbs))
    scoped_pdu = encode_sequence([encode_string(ENGINE_ID), encode_string(b''), pdu])

    return encode_sequence([
        encode_integer(3),
        global_data,
        sec_params,
        scoped_pdu,
    ])


# ═══════════════════════════════════════════════════════════════════════════
# Simulated MIB Data
# ═══════════════════════════════════════════════════════════════════════════

BOOT_TIME = time.time()
DEVICE_NAME = "SnmpLens-TestDevice"
DEVICE_DESCR = "SnmpLens Simulated Agent v2.0 (Python) - Linux 5.15.0 x86_64"
DEVICE_CONTACT = "admin@snmplens-test.local"
DEVICE_LOCATION = "Lab - Rack 42, Unit 7"

INTERFACES = [
    {"name": "lo",    "descr": "Loopback",           "type": 24, "speed": 0,          "admin": 1, "oper": 1, "mac": b'\x00\x00\x00\x00\x00\x00'},
    {"name": "eth0",  "descr": "Gigabit Ethernet 0",  "type": 6,  "speed": 1000000000, "admin": 1, "oper": 1, "mac": b'\x00\x11\x22\x33\x44\x55'},
    {"name": "eth1",  "descr": "Gigabit Ethernet 1",  "type": 6,  "speed": 1000000000, "admin": 1, "oper": 1, "mac": b'\x00\x11\x22\x33\x44\x66'},
    {"name": "eth2",  "descr": "Gigabit Ethernet 2",  "type": 6,  "speed": 1000000000, "admin": 1, "oper": 2, "mac": b'\x00\x11\x22\x33\x44\x77'},
    {"name": "wlan0", "descr": "WiFi 802.11ac",       "type": 71, "speed": 300000000,  "admin": 1, "oper": 1, "mac": b'\xAA\xBB\xCC\xDD\x00\x11'},
]

IF_COUNTERS = {}
for _i, _iface in enumerate(INTERFACES, 1):
    IF_COUNTERS[_i] = {
        "inOctets": random.randint(100000, 90000000),
        "outOctets": random.randint(100000, 90000000),
        "inPkts": random.randint(1000, 500000),
        "outPkts": random.randint(1000, 500000),
        "inErrors": random.randint(0, 50),
        "outErrors": random.randint(0, 10),
        "inDiscards": random.randint(0, 20),
        "outDiscards": random.randint(0, 5),
    }


def uptime_ticks():
    return int((time.time() - BOOT_TIME) * 100)


def grow_counters():
    for idx, iface in enumerate(INTERFACES, 1):
        if iface["oper"] == 1:
            sf = iface["speed"] / 1000000000 if iface["speed"] else 0.01
            IF_COUNTERS[idx]["inOctets"] += int(random.uniform(5000, 200000) * sf)
            IF_COUNTERS[idx]["outOctets"] += int(random.uniform(3000, 150000) * sf)
            IF_COUNTERS[idx]["inPkts"] += int(random.uniform(10, 500) * sf)
            IF_COUNTERS[idx]["outPkts"] += int(random.uniform(8, 400) * sf)
            if random.random() < 0.02:
                IF_COUNTERS[idx]["inErrors"] += 1
            if random.random() < 0.005:
                IF_COUNTERS[idx]["outErrors"] += 1


def build_oid_tree():
    grow_counters()
    t = {}

    def add(oid, tag, val):
        t[oid] = (tag, val)

    add("1.3.6.1.2.1.1.1.0", OCTET_STRING, DEVICE_DESCR.encode())
    add("1.3.6.1.2.1.1.2.0", OID_TAG, "1.3.6.1.4.1.99999.1.1")
    add("1.3.6.1.2.1.1.3.0", TIMETICKS, uptime_ticks())
    add("1.3.6.1.2.1.1.4.0", OCTET_STRING, DEVICE_CONTACT.encode())
    add("1.3.6.1.2.1.1.5.0", OCTET_STRING, DEVICE_NAME.encode())
    add("1.3.6.1.2.1.1.6.0", OCTET_STRING, DEVICE_LOCATION.encode())
    add("1.3.6.1.2.1.1.7.0", INTEGER, 72)
    add("1.3.6.1.2.1.2.1.0", INTEGER, len(INTERFACES))

    for idx, iface in enumerate(INTERFACES, 1):
        p = "1.3.6.1.2.1.2.2.1"
        c = IF_COUNTERS[idx]
        add(f"{p}.1.{idx}", INTEGER, idx)
        add(f"{p}.2.{idx}", OCTET_STRING, iface["descr"].encode())
        add(f"{p}.3.{idx}", INTEGER, iface["type"])
        add(f"{p}.4.{idx}", INTEGER, 1500 if iface["type"] != 24 else 65536)
        add(f"{p}.5.{idx}", GAUGE32, iface["speed"])
        add(f"{p}.6.{idx}", OCTET_STRING, iface["mac"])
        add(f"{p}.7.{idx}", INTEGER, iface["admin"])
        add(f"{p}.8.{idx}", INTEGER, iface["oper"])
        add(f"{p}.9.{idx}", TIMETICKS, max(0, uptime_ticks() - random.randint(0, 10000)))
        add(f"{p}.10.{idx}", COUNTER32, c["inOctets"] % (2**32))
        add(f"{p}.11.{idx}", COUNTER32, c["inPkts"] % (2**32))
        add(f"{p}.13.{idx}", COUNTER32, c["inDiscards"])
        add(f"{p}.14.{idx}", COUNTER32, c["inErrors"])
        add(f"{p}.16.{idx}", COUNTER32, c["outOctets"] % (2**32))
        add(f"{p}.17.{idx}", COUNTER32, c["outPkts"] % (2**32))
        add(f"{p}.19.{idx}", COUNTER32, c["outDiscards"])
        add(f"{p}.20.{idx}", COUNTER32, c["outErrors"])

    add("1.3.6.1.2.1.4.1.0", INTEGER, 1)
    add("1.3.6.1.2.1.4.2.0", INTEGER, 64)
    add("1.3.6.1.2.1.4.3.0", COUNTER32, random.randint(100000, 999999))
    add("1.3.6.1.2.1.11.1.0", COUNTER32, random.randint(1000, 50000))
    add("1.3.6.1.2.1.11.2.0", COUNTER32, random.randint(1000, 50000))

    add("1.3.6.1.2.1.25.1.1.0", TIMETICKS, uptime_ticks())
    add("1.3.6.1.2.1.25.1.2.0", OCTET_STRING, datetime.now().strftime("%Y-%m-%d %H:%M:%S").encode())
    add("1.3.6.1.2.1.25.1.5.0", GAUGE32, random.randint(50, 200))
    add("1.3.6.1.2.1.25.1.6.0", GAUGE32, random.randint(100, 400))

    add("1.3.6.1.2.1.25.2.3.1.1.1", INTEGER, 1)
    add("1.3.6.1.2.1.25.2.3.1.3.1", OCTET_STRING, b"Physical Memory")
    add("1.3.6.1.2.1.25.2.3.1.4.1", INTEGER, 1024)
    add("1.3.6.1.2.1.25.2.3.1.5.1", INTEGER, 8388608)
    add("1.3.6.1.2.1.25.2.3.1.6.1", INTEGER, random.randint(3000000, 7000000))
    add("1.3.6.1.2.1.25.2.3.1.1.2", INTEGER, 2)
    add("1.3.6.1.2.1.25.2.3.1.3.2", OCTET_STRING, b"/dev/sda1 - Root Filesystem")
    add("1.3.6.1.2.1.25.2.3.1.4.2", INTEGER, 4096)
    add("1.3.6.1.2.1.25.2.3.1.5.2", INTEGER, 125000000)
    add("1.3.6.1.2.1.25.2.3.1.6.2", INTEGER, random.randint(30000000, 90000000))

    for idx, iface in enumerate(INTERFACES, 1):
        p = "1.3.6.1.2.1.31.1.1.1"
        c = IF_COUNTERS[idx]
        add(f"{p}.1.{idx}", OCTET_STRING, iface["name"].encode())
        add(f"{p}.6.{idx}", COUNTER64, c["inOctets"])
        add(f"{p}.10.{idx}", COUNTER64, c["outOctets"])
        add(f"{p}.15.{idx}", GAUGE32, iface["speed"] // 1000000 if iface["speed"] else 0)
        add(f"{p}.18.{idx}", OCTET_STRING, iface["descr"].encode())

    return t


def oid_tuple(oid_str):
    return tuple(int(x) for x in oid_str.strip('.').split('.'))


# ═══════════════════════════════════════════════════════════════════════════
# SNMP Request Processing
# ═══════════════════════════════════════════════════════════════════════════

VALID_COMMUNITIES = {"public", "private"}


def process_varbinds(pdu_type, varbinds, non_rep, max_rep):
    """Process varbinds for any SNMP version. Returns list of encoded varbind bytes."""
    tree = build_oid_tree()
    sorted_oids = sorted(tree.keys(), key=oid_tuple)

    def find_next(oid_str):
        target = oid_tuple(oid_str)
        for candidate in sorted_oids:
            if oid_tuple(candidate) > target:
                return candidate
        return None

    response_vbs = []

    if pdu_type == GET_REQUEST:
        for oid_str, _, _ in varbinds:
            if oid_str in tree:
                tag, val = tree[oid_str]
                response_vbs.append(encode_varbind(oid_str, encode_value(tag, val)))
            else:
                response_vbs.append(encode_varbind(oid_str, encode_tlv(NO_SUCH_INSTANCE, b'')))

    elif pdu_type == GET_NEXT_REQUEST:
        for oid_str, _, _ in varbinds:
            next_oid = find_next(oid_str)
            if next_oid:
                tag, val = tree[next_oid]
                response_vbs.append(encode_varbind(next_oid, encode_value(tag, val)))
            else:
                response_vbs.append(encode_varbind(oid_str, encode_tlv(END_OF_MIB_VIEW, b'')))

    elif pdu_type == GET_BULK_REQUEST:
        for i in range(min(non_rep, len(varbinds))):
            oid_str = varbinds[i][0]
            next_oid = find_next(oid_str)
            if next_oid:
                tag, val = tree[next_oid]
                response_vbs.append(encode_varbind(next_oid, encode_value(tag, val)))
            else:
                response_vbs.append(encode_varbind(oid_str, encode_tlv(END_OF_MIB_VIEW, b'')))
        for i in range(non_rep, len(varbinds)):
            current = varbinds[i][0]
            for _ in range(max(max_rep, 1)):
                next_oid = find_next(current)
                if next_oid:
                    tag, val = tree[next_oid]
                    response_vbs.append(encode_varbind(next_oid, encode_value(tag, val)))
                    current = next_oid
                else:
                    response_vbs.append(encode_varbind(current, encode_tlv(END_OF_MIB_VIEW, b'')))
                    break

    elif pdu_type == SET_REQUEST:
        for oid_str, val_tag, val_raw in varbinds:
            if oid_str in tree:
                tag, val = tree[oid_str]
                response_vbs.append(encode_varbind(oid_str, encode_value(tag, val)))
            else:
                response_vbs.append(encode_varbind(oid_str, encode_tlv(NO_SUCH_INSTANCE, b'')))

    return response_vbs


def decode_v1v2_message(data):
    """Decode SNMPv1/v2c message."""
    _, msg_data, _ = decode_tlv(data, 0)
    off = 0
    _, ver_raw, off = decode_tlv(msg_data, off)
    version = decode_integer(ver_raw)
    _, comm_raw, off = decode_tlv(msg_data, off)
    community = comm_raw.decode('ascii', errors='replace')
    pdu_tag, pdu_data, _ = decode_tlv(msg_data, off)

    pdu_off = 0
    _, reqid_raw, pdu_off = decode_tlv(pdu_data, pdu_off)
    request_id = decode_integer(reqid_raw)
    _, field2_raw, pdu_off = decode_tlv(pdu_data, pdu_off)
    _, field3_raw, pdu_off = decode_tlv(pdu_data, pdu_off)
    non_rep = decode_integer(field2_raw) if pdu_tag == GET_BULK_REQUEST else 0
    max_rep = decode_integer(field3_raw) if pdu_tag == GET_BULK_REQUEST else 0
    _, vb_list_data, _ = decode_tlv(pdu_data, pdu_off)
    varbinds = decode_varbind_list(vb_list_data)

    return version, community, pdu_tag, request_id, varbinds, non_rep, max_rep


def encode_v1v2_response(version, community, request_id, varbind_bytes_list, error_status=0, error_index=0):
    vb_seq = encode_sequence(varbind_bytes_list)
    pdu = encode_tlv(GET_RESPONSE, encode_integer(request_id) + encode_integer(error_status) + encode_integer(error_index) + vb_seq)
    return encode_sequence([encode_integer(version), encode_string(community), pdu])


def handle_request(data, addr):
    """Route incoming SNMP packet to v1/v2c or v3 handler."""
    try:
        # Peek at version
        _, msg_data, _ = decode_tlv(data, 0)
        _, ver_raw, _ = decode_tlv(msg_data, 0)
        version = decode_integer(ver_raw)
    except Exception:
        return None

    if version == 3:
        return handle_v3_request(data, addr)
    else:
        return handle_v1v2_request(data, addr)


def handle_v1v2_request(data, addr):
    try:
        version, community, pdu_type, request_id, varbinds, non_rep, max_rep = decode_v1v2_message(data)
    except Exception:
        return None

    if community not in VALID_COMMUNITIES:
        return None

    response_vbs = process_varbinds(pdu_type, varbinds, non_rep, max_rep)
    return encode_v1v2_response(version, community, request_id, response_vbs)


def handle_v3_request(data, addr):
    try:
        parsed = decode_snmpv3_message(data)
    except Exception as e:
        print(f"  [V3] Failed to decode: {e}")
        return None

    usm = parsed['usm']
    username = usm['username']
    msg_flags = parsed['msg_flags']

    # Discovery: empty username or unknown engineID
    if not username or usm['engine_id'] != ENGINE_ID:
        return build_v3_discovery_response(parsed['msg_id'])

    # Lookup user
    user_info = V3_USERS.get(username)
    if not user_info:
        print(f"  [V3] Unknown user: {username}")
        return build_v3_discovery_response(parsed['msg_id'])

    # Verify authentication if required
    if msg_flags & 0x01:
        if not user_info.get('auth_key'):
            print(f"  [V3] User '{username}' has no auth key")
            return None
        if not verify_auth(data, parsed, user_info):
            print(f"  [V3] Auth failed for user '{username}'")
            return None

    # Decode scoped PDU (decrypt if needed)
    try:
        ctx_eid, ctx_name, pdu_type, request_id, varbinds, non_rep, max_rep = decode_scoped_pdu(data, parsed, user_info)
    except Exception as e:
        print(f"  [V3] Failed to decode scoped PDU: {e}")
        return None

    # Process the request
    response_vbs = process_varbinds(pdu_type, varbinds, non_rep, max_rep)

    # Build authenticated/encrypted response
    # Override msg_id in parsed so build_v3_response uses the correct request_id
    resp_parsed = {**parsed, 'msg_id': request_id}
    return build_v3_response(resp_parsed, response_vbs, user_info)


# ═══════════════════════════════════════════════════════════════════════════
# Trap Sender
# ═══════════════════════════════════════════════════════════════════════════

TRAP_DEFS = [
    ("1.3.6.1.6.3.1.1.5.3", "linkDown"),
    ("1.3.6.1.6.3.1.1.5.4", "linkUp"),
    ("1.3.6.1.6.3.1.1.5.1", "coldStart"),
    ("1.3.6.1.4.1.99999.0.1", "customAlert"),
]


def build_trap_varbinds():
    """Pick a random trap type and build its varbinds."""
    trap_oid, trap_name = random.choice(TRAP_DEFS)
    extra_vbs = []
    if "link" in trap_name.lower():
        extra_vbs = [
            encode_varbind("1.3.6.1.2.1.2.2.1.1.3", encode_value(INTEGER, 3)),
            encode_varbind("1.3.6.1.2.1.2.2.1.8.3", encode_value(INTEGER, 2 if "Down" in trap_name else 1)),
        ]
    elif trap_name == "customAlert":
        extra_vbs = [
            encode_varbind("1.3.6.1.4.1.99999.1.1", encode_value(OCTET_STRING, f"Alert at {datetime.now().strftime('%H:%M:%S')}".encode())),
            encode_varbind("1.3.6.1.4.1.99999.1.2", encode_value(INTEGER, random.choice([1, 2, 3]))),
        ]
    vbs = [
        encode_varbind("1.3.6.1.2.1.1.3.0", encode_value(TIMETICKS, uptime_ticks())),
        encode_varbind("1.3.6.1.6.3.1.1.4.1.0", encode_oid(trap_oid)),
    ] + extra_vbs
    return trap_name, vbs


def encode_snmp_v2_trap(community, request_id, varbind_bytes_list):
    vb_seq = encode_sequence(varbind_bytes_list)
    pdu = encode_tlv(TRAP_V2, encode_integer(request_id) + encode_integer(0) + encode_integer(0) + vb_seq)
    return encode_sequence([encode_integer(1), encode_string(community), pdu])


def encode_snmp_v3_trap(user_info, username, request_id, varbind_bytes_list):
    """Build an SNMPv3 authenticated+encrypted trap packet."""
    algo = user_info.get('auth_algo', 'sha1')
    key_len = user_info.get('priv_key_len', 16)
    has_auth = user_info.get('auth_key') is not None
    has_priv = user_info.get('priv_key') is not None and HAS_AES

    msg_flags = 0x00
    if has_auth:
        msg_flags |= 0x01
    if has_priv:
        msg_flags |= 0x02

    # Build scoped PDU with trap PDU inside
    vb_seq = encode_sequence(varbind_bytes_list)
    pdu = encode_tlv(TRAP_V2, encode_integer(request_id) + encode_integer(0) + encode_integer(0) + vb_seq)
    scoped_pdu = encode_sequence([
        encode_string(ENGINE_ID),
        encode_string(b''),
        pdu,
    ])

    # Encrypt if privacy enabled
    priv_params = b''
    if has_priv:
        salt = next_salt()
        priv_params = struct.pack('>Q', salt)
        encrypted = aes_encrypt(user_info['priv_key'], ENGINE_BOOTS, engine_time(), salt, scoped_pdu, key_len)
        msg_data_part = encode_string(encrypted)
    else:
        msg_data_part = scoped_pdu

    # Auth placeholder
    auth_len = get_auth_param_len(algo) if has_auth else 0
    auth_placeholder = b'\x00' * auth_len

    sec_params = encode_usm_security_params(
        ENGINE_ID, ENGINE_BOOTS, engine_time(), username, auth_placeholder, priv_params
    )

    global_data = encode_sequence([
        encode_integer(random.randint(1, 2**31)),  # msgID
        encode_integer(65507),
        encode_string(bytes([msg_flags])),
        encode_integer(3),  # USM
    ])

    full_msg = encode_sequence([
        encode_integer(3),
        global_data,
        sec_params,
        msg_data_part,
    ])

    # Compute and insert HMAC
    if has_auth:
        mac = compute_auth_mac(user_info['auth_key'], full_msg, algo)
        idx = full_msg.find(auth_placeholder)
        if idx >= 0:
            full_msg = full_msg[:idx] + mac + full_msg[idx+auth_len:]

    return full_msg


async def trap_sender(target, port, community, interval):
    print(f"  Trap sender  : v2c + v3 traps to {target}:{port} every {interval}s")
    await asyncio.sleep(3)
    loop = asyncio.get_event_loop()
    transport = None

    # Collect v3 users that have auth for v3 trap sending
    v3_trap_users = [(name, info) for name, info in V3_USERS.items() if info.get('auth_key')]

    while True:
        try:
            trap_name, vbs = build_trap_varbinds()

            # Alternate between v2c and v3 traps
            use_v3 = v3_trap_users and random.random() < 0.5
            if use_v3:
                username, user_info = random.choice(v3_trap_users)
                pkt = encode_snmp_v3_trap(user_info, username, random.randint(1, 2**31), vbs)
                version_label = f"v3/{username}"
            else:
                pkt = encode_snmp_v2_trap(community, random.randint(1, 2**31), vbs)
                version_label = "v2c"

            if transport is None:
                transport, _ = await loop.create_datagram_endpoint(asyncio.DatagramProtocol, remote_addr=(target, port))
            transport.sendto(pkt)
            print(f"  [TRAP] Sent {trap_name} ({version_label}) -> {target}:{port}")
        except Exception as e:
            print(f"  [TRAP] Failed: {e}")
            transport = None
        await asyncio.sleep(interval)


# ═══════════════════════════════════════════════════════════════════════════
# UDP Server
# ═══════════════════════════════════════════════════════════════════════════

class SnmpProtocol(asyncio.DatagramProtocol):
    def __init__(self):
        self.transport = None
        self.request_count = 0

    def connection_made(self, transport):
        self.transport = transport

    def datagram_received(self, data, addr):
        self.request_count += 1
        response = handle_request(data, addr)
        if response:
            self.transport.sendto(response, addr)


# ═══════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════

async def run(args):
    loop = asyncio.get_event_loop()
    _, protocol = await loop.create_datagram_endpoint(SnmpProtocol, local_addr=("0.0.0.0", args.port))

    print("=" * 60)
    print("  SnmpLens Test Agent v2.0 (SNMPv1/v2c/v3)")
    print("=" * 60)
    print(f"  Agent port   : {args.port}")
    print(f"  Device       : {DEVICE_NAME}")
    print(f"  v1/v2c       : community 'public' (ro), 'private' (rw)")
    print(f"  v3 SHA/AES128  : user 'snmplens'   authpass123 / privpass123")
    print(f"  v3 SHA256/AES128: user 'sha256user' authpass123 / privpass123")
    print(f"  v3 SHA512/AES256: user 'sha512user' authpass123 / privpass123")
    print(f"  v3 authNoPriv  : user 'authonly'    authpass123")
    print(f"  v3 noAuth      : user 'noauthuser'")
    print(f"  AES support    : {'YES' if HAS_AES else 'NO (pip install pycryptodome)'}")
    print(f"  Interfaces     : {len(INTERFACES)} simulated")
    print(f"  OIDs           : system, ifTable, ifXTable, IP, SNMP, hrSystem, hrStorage")
    print()
    print("  SnmpLens settings examples:")
    print(f"    Target : 127.0.0.1    Port : {args.port}")
    print(f"    v2c    : community 'public'")
    print(f"    v3 max : user 'sha512user', SHA-512 / authpass123, AES-256 / privpass123")
    print()

    if not args.no_traps:
        asyncio.ensure_future(trap_sender(args.trap_target, args.trap_port, args.community, args.trap_interval))
    else:
        print("  Traps        : disabled")

    print("  Agent running... Press Ctrl+C to stop.")
    print("=" * 60)

    try:
        await asyncio.Future()
    except asyncio.CancelledError:
        pass


def main():
    parser = argparse.ArgumentParser(
        description="SnmpLens Test Agent — pure Python SNMP agent simulator (v1/v2c/v3)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Credentials:
  v1/v2c  community "public" (ro) / "private" (rw)
  v3      user "snmplens"   authPriv   SHA-1/authpass123    AES-128/privpass123
  v3      user "sha256user" authPriv   SHA-256/authpass123  AES-128/privpass123
  v3      user "sha512user" authPriv   SHA-512/authpass123  AES-256/privpass123
  v3      user "authonly"   authNoPriv SHA-1/authpass123
  v3      user "noauthuser" noAuthNoPriv

Examples:
  python snmp_test_agent.py
  python snmp_test_agent.py --port 1161 --trap-interval 15
  python snmp_test_agent.py --no-traps
""",
    )
    parser.add_argument("--port", type=int, default=1161, help="Agent listen port (default: 1161)")
    parser.add_argument("--trap-target", default="127.0.0.1", help="Trap destination IP")
    parser.add_argument("--trap-port", type=int, default=162, help="Trap destination port")
    parser.add_argument("--trap-interval", type=int, default=30, help="Seconds between traps")
    parser.add_argument("--no-traps", action="store_true", help="Disable trap sending")
    parser.add_argument("--community", default="public", help="v1/v2c community")
    args = parser.parse_args()

    # Initialize SNMPv3 users with derived keys
    global V3_USERS

    # User 1: authPriv — SHA-1 + AES-128
    V3_USERS["snmplens"] = {
        'auth_key': derive_keys("authpass123", ENGINE_ID, 'sha1'),
        'priv_key': derive_keys("privpass123", ENGINE_ID, 'sha1'),
        'auth_algo': 'sha1',
        'priv_key_len': 16,  # AES-128
        'level': AUTH_PRIV,
    }

    # User 2: authPriv — SHA-256 + AES-128
    V3_USERS["sha256user"] = {
        'auth_key': derive_keys("authpass123", ENGINE_ID, 'sha256'),
        'priv_key': derive_keys("privpass123", ENGINE_ID, 'sha256'),
        'auth_algo': 'sha256',
        'priv_key_len': 16,  # AES-128
        'level': AUTH_PRIV,
    }

    # User 3: authPriv — SHA-512 + AES-256 (max security)
    V3_USERS["sha512user"] = {
        'auth_key': derive_keys("authpass123", ENGINE_ID, 'sha512'),
        'priv_key': derive_keys("privpass123", ENGINE_ID, 'sha512'),
        'auth_algo': 'sha512',
        'priv_key_len': 32,  # AES-256
        'level': AUTH_PRIV,
    }

    # User 4: authNoPriv — SHA-1
    V3_USERS["authonly"] = {
        'auth_key': derive_keys("authpass123", ENGINE_ID, 'sha1'),
        'priv_key': None,
        'auth_algo': 'sha1',
        'priv_key_len': 0,
        'level': AUTH_NO_PRIV,
    }

    # User 5: noAuthNoPriv
    V3_USERS["noauthuser"] = {
        'auth_key': None,
        'priv_key': None,
        'auth_algo': None,
        'priv_key_len': 0,
        'level': AUTH_NO_AUTH_NO_PRIV,
    }

    try:
        asyncio.run(run(args))
    except KeyboardInterrupt:
        print("\nShutting down...")


if __name__ == "__main__":
    main()
