package storage

import (
	"testing"
	"time"
)

// A credential profile carries an SNMP version, so replacing a session's
// connection may change its version too — and must leave it alone when none is
// given. The profile id travels in the stored connection, in the clear, which is
// only acceptable because it is an id and never a credential.
func TestUpdateSessionConnCanChangeTheVersion(t *testing.T) {
	st := newTestStorage(t)
	now := time.Now().UTC().Format(time.RFC3339)
	id, err := st.CreateSession("", "1.1", []string{"10.0.0.1"}, 5000, "v2c", now, nil,
		&SessionConn{Port: 161, Profile: "p-core"}, nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := st.UpdateSessionConn(id, "v3", &SessionConn{Port: 161, Profile: "p-core", V3User: "ops"}); err != nil {
		t.Fatalf("UpdateSessionConn: %v", err)
	}
	sessions, _ := st.ListSessions()
	if got := sessions[0]; got.SnmpVersion != "v3" || got.Conn == nil ||
		got.Conn.Profile != "p-core" || got.Conn.V3User != "ops" {
		t.Fatalf("not updated: version %q, conn %+v", got.SnmpVersion, got.Conn)
	}

	if err := st.UpdateSessionConn(id, "", &SessionConn{Port: 1161, Profile: "p-core"}); err != nil {
		t.Fatalf("UpdateSessionConn: %v", err)
	}
	sessions, _ = st.ListSessions()
	if got := sessions[0]; got.SnmpVersion != "v3" || got.Conn.Port != 1161 {
		t.Errorf("an empty version must keep v3 and still replace the connection: version %q, conn %+v",
			got.SnmpVersion, got.Conn)
	}
}
