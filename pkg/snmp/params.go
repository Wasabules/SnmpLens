package snmp

// SnmpRequest groups common SNMP parameters into a single struct for Wails bindings.
type SnmpRequest struct {
	Targets   []string `json:"targets"`
	OID       string   `json:"oid"`
	Community string   `json:"community"`
	Version   string   `json:"version"`
	Port      int      `json:"port"`
	Timeout   int      `json:"timeout"`
	Retries   int      `json:"retries"`
	V3        V3Params `json:"v3"`
}

// SetRequest extends SnmpRequest with SET-specific fields.
type SetRequest struct {
	SnmpRequest
	Value     string `json:"value"`
	ValueType string `json:"valueType"`
}

// GetBulkRequest extends SnmpRequest with GETBULK-specific fields.
type GetBulkRequest struct {
	SnmpRequest
	NonRepeaters   int `json:"nonRepeaters"`
	MaxRepetitions int `json:"maxRepetitions"`
}

// TestRequest holds parameters for a single-target connection test.
type TestRequest struct {
	Target    string   `json:"target"`
	Community string   `json:"community"`
	Version   string   `json:"version"`
	Port      int      `json:"port"`
	Timeout   int      `json:"timeout"`
	V3        V3Params `json:"v3"`
}

// DiscoverRequest holds parameters for CIDR network discovery.
type DiscoverRequest struct {
	CIDR      string   `json:"cidr"`
	Community string   `json:"community"`
	Version   string   `json:"version"`
	Port      int      `json:"port"`
	Timeout   int      `json:"timeout"`
	V3        V3Params `json:"v3"`
}

// TrapListenerRequest holds parameters for starting a trap listener.
type TrapListenerRequest struct {
	Port int      `json:"port"`
	V3   V3Params `json:"v3"`
}
