// GENERATED seed for the event journal, lifted from the specification.
// The mix of kinds is the valuable part; dynamic.js multiplies it.

export const EVENT_SEED = {
 "items": [
  {
   "seq": 18422,
   "id": "b1c7f0a2-4d3e-4a91-9b60-2f8c1d5e7a03",
   "ts": "2026-09-05T14:41:07Z",
   "category": "trap",
   "kind": "trap.received",
   "severity": "minor",
   "state": "oneshot",
   "source": "10.20.4.11",
   "oid": ".1.3.6.1.6.3.1.1.5.3",
   "dedupKey": "trap|10.20.4.11|.1.3.6.1.6.3.1.1.5.3",
   "titleKey": "events.kind.trap.received",
   "params": {
    "source": "10.20.4.11",
    "version": "2c",
    "pduType": "Trap",
    "varbinds": 6,
    "trapOid": ".1.3.6.1.6.3.1.1.5.3"
   },
   "summary": "Trap from 10.20.4.11 (2c, 6 varbinds)",
   "payloadSize": 487,
   "acked": false
  },
  {
   "seq": 18421,
   "id": "6d0a94f7-1b52-4c8e-a0d1-77e3b9c41d55",
   "ts": "2026-09-05T14:38:52Z",
   "category": "threshold",
   "kind": "threshold.opened",
   "severity": "major",
   "state": "open",
   "source": "10.20.4.1",
   "oid": "1.3.6.1.2.1.2.2.1.10.3",
   "sessionId": "9f2c1e34-88ab-4d70-9c31-5b0e6a2f1c47",
   "sessionName": "Datacenter core",
   "dedupKey": "th|9f2c1e34|10.20.4.1|1.3.6.1.2.1.2.2.1.10.3|above",
   "corrId": "c4a1f9e2b7d3",
   "titleKey": "events.kind.threshold.opened",
   "params": {
    "target": "10.20.4.1",
    "oid": "1.3.6.1.2.1.2.2.1.10.3",
    "kind": "above",
    "bound": 85,
    "value": 93.4,
    "heldSeconds": 120,
    "forSeconds": 60
   },
   "summary": "1.3.6.1.2.1.2.2.1.10.3 on 10.20.4.1 is above 85 (value 93.4, held 120s)",
   "value": 93.4,
   "payloadSize": 0,
   "acked": false
  },
  {
   "seq": 18418,
   "id": "3ac6e281-9f04-42bb-8e7a-1d5c0b93af62",
   "ts": "2026-09-05T14:27:03Z",
   "category": "reachability",
   "kind": "reachability.down",
   "severity": "major",
   "state": "open",
   "source": "10.20.4.77",
   "sessionId": "9f2c1e34-88ab-4d70-9c31-5b0e6a2f1c47",
   "sessionName": "Datacenter core",
   "dedupKey": "reach|9f2c1e34|10.20.4.77",
   "titleKey": "events.kind.reachability.down",
   "params": {
    "target": "10.20.4.77",
    "error": "request timeout (after 1 retries)"
   },
   "summary": "10.20.4.77 stopped responding",
   "payloadSize": 0,
   "acked": false
  },
  {
   "seq": 18415,
   "id": "f70b3d19-5c62-4ae8-b1f4-93a0d2e6c118",
   "ts": "2026-09-05T14:19:41Z",
   "category": "system",
   "kind": "system.sink_dead_letter",
   "severity": "major",
   "state": "oneshot",
   "titleKey": "events.kind.system.sink_dead_letter",
   "params": {
    "sink": "NOC syslog relay",
    "sinkId": "a2f19c0e-7d44-4b16-99cf-0e1b7d2a5c83",
    "attempts": 6,
    "error": "dial tcp 10.20.9.4:6514: i/o timeout"
   },
   "summary": "Delivery to NOC syslog relay given up: dial tcp 10.20.9.4:6514: i/o timeout",
   "payloadSize": 0,
   "acked": false
  },
  {
   "seq": 18409,
   "id": "2e84c5b0-6a37-4f92-85dd-c1b70e94f206",
   "ts": "2026-09-05T13:58:12Z",
   "category": "trap",
   "kind": "trap.inform",
   "severity": "minor",
   "state": "oneshot",
   "source": "10.20.4.23",
   "oid": ".1.3.6.1.4.1.318.0.5",
   "dedupKey": "trap|10.20.4.23|.1.3.6.1.4.1.318.0.5",
   "titleKey": "events.kind.trap.inform",
   "params": {
    "source": "10.20.4.23",
    "version": "2c",
    "pduType": "Inform",
    "varbinds": 5,
    "trapOid": ".1.3.6.1.4.1.318.0.5"
   },
   "summary": "Inform from 10.20.4.23 (2c, 5 varbinds)",
   "payloadSize": 356,
   "acked": false
  },
  {
   "seq": 18402,
   "id": "8b1d67a4-30e9-4c55-b2a8-4f60d18c9e37",
   "ts": "2026-09-05T13:31:55Z",
   "category": "threshold",
   "kind": "threshold.resolved",
   "severity": "info",
   "state": "resolved",
   "source": "10.20.4.1",
   "oid": "1.3.6.1.2.1.2.2.1.10.3",
   "sessionId": "9f2c1e34-88ab-4d70-9c31-5b0e6a2f1c47",
   "sessionName": "Datacenter core",
   "dedupKey": "th|9f2c1e34|10.20.4.1|1.3.6.1.2.1.2.2.1.10.3|above",
   "corrId": "c4a1f9e2b7d3",
   "titleKey": "events.kind.threshold.resolved",
   "params": {
    "target": "10.20.4.1",
    "oid": "1.3.6.1.2.1.2.2.1.10.3",
    "value": 61.2
   },
   "summary": "1.3.6.1.2.1.2.2.1.10.3 on 10.20.4.1 is back within range (value 61.2)",
   "value": 61.2,
   "payloadSize": 0,
   "acked": true
  },
  {
   "seq": 18400,
   "id": "cd4290f8-b16e-47a3-90c2-7ae5138b6d04",
   "ts": "2026-09-05T13:22:08Z",
   "category": "reachability",
   "kind": "reachability.up",
   "severity": "info",
   "state": "resolved",
   "source": "192.168.30.5",
   "sessionId": "9f2c1e34-88ab-4d70-9c31-5b0e6a2f1c47",
   "sessionName": "Datacenter core",
   "dedupKey": "reach|9f2c1e34|192.168.30.5",
   "titleKey": "events.kind.reachability.up",
   "params": {
    "target": "192.168.30.5",
    "error": ""
   },
   "summary": "192.168.30.5 is responding again",
   "payloadSize": 0,
   "acked": true
  },
  {
   "seq": 18399,
   "id": "51ec8a37-4d90-4b61-8f0e-b2c473d915aa",
   "ts": "2026-09-05T13:04:30Z",
   "category": "system",
   "kind": "system.info",
   "severity": "info",
   "state": "oneshot",
   "titleKey": "events.kind.system.info",
   "params": {},
   "summary": "Resumed 3 monitoring session(s) after startup.",
   "payloadSize": 0,
   "acked": true
  }
 ],
 "nextCursor": 18399,
 "total": 1284
};
