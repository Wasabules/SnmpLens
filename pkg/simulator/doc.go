// Package simulator runs simulated SNMP agents: devices that answer GET,
// GETNEXT and GETBULK in v1, v2c and v3 from a tree of objects, so SnmpLens can
// be exercised, demonstrated and tested without a switch on the desk.
//
// The codec is gosnmp's, in both directions. What this package owns is what an
// agent DECIDES, which gosnmp — written as a manager and a trap receiver — never
// had to: which object answers, which error a request earns, and which Report a
// v3 message gets instead of an answer.
//
// Two rules carry the security, and each is a defect in the obvious version.
//
// An agent listens on a loopback address and nowhere else (CheckListen). The
// rule is applied where the address is parsed AND on the socket once bound,
// because a rule applied only where a device is saved is one an imported file,
// or the next code path, walks around.
//
// The checks an authoritative engine makes before trusting a message come
// FIRST, here, from the message's header (peekV3), and only then is gosnmp asked
// to authenticate and decrypt it. gosnmp's receive path was written for a trap
// receiver, and three of its behaviours are wrong for an agent:
//
//   - it re-localises its keys to whatever engine ID the message names, so a
//     message addressed to ANOTHER engine authenticates here — which is exactly
//     what localised keys exist to prevent;
//   - asked to trust the message's own parameters, it takes the security level
//     from the message too, so a noAuthNoPriv message naming an authPriv user is
//     not tested at all;
//   - a message with an empty user and an empty engine ID skips authentication
//     outright, whatever its flags say (its reading of RFC 3414 discovery).
//
// So the engine ID, the user and the level are settled before gosnmp sees a
// byte, and by then none of the three can be reached.
package simulator
