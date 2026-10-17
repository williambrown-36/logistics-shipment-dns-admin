package logistics

import "testing"

func TestDecideRecord(t *testing.T) {
	tests := []struct {
		name        string
		event       ShipmentEvent
		wantAction  string
		wantType    string
		wantName    string
		wantContent string
		wantErr     bool
	}{
		{
			name:       "new tracking address creates a cname",
			event:      ShipmentEvent{EventID: "evt-1", ShipmentID: "PKG-42", Domain: "ops.example.com", Status: StatusInTransit, TrackingHost: "tracking.carrier.example"},
			wantAction: "create", wantType: "CNAME", wantName: "track-pkg-42", wantContent: "tracking.carrier.example",
		},
		{
			name:       "proof replaces an existing record",
			event:      ShipmentEvent{EventID: "evt-2", ShipmentID: "PKG-42", Domain: "ops.example.com", Status: StatusDelivered, ExistingRecordID: "rec-7", Proof: &ProofOfDelivery{ObjectKey: "proofs/pkg-42.pdf", SHA256: "abc123"}},
			wantAction: "update", wantType: "TXT", wantName: "_pod.pkg-42", wantContent: "object=proofs/pkg-42.pdf;sha256=abc123",
		},
		{
			name:       "exception is visible as a txt record",
			event:      ShipmentEvent{EventID: "evt-3", ShipmentID: "PKG-42", Domain: "ops.example.com", Status: StatusException, Exception: &DeliveryException{Code: "CUSTOMS_HOLD", Note: "manual review"}},
			wantAction: "create", wantType: "TXT", wantName: "_exception.pkg-42", wantContent: "code=CUSTOMS_HOLD;note=manual review",
		},
		{
			name:    "delivered requires immutable proof details",
			event:   ShipmentEvent{EventID: "evt-4", ShipmentID: "PKG-42", Domain: "ops.example.com", Status: StatusDelivered},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecideRecord(tt.event)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecideRecord: %v", err)
			}
			if got.Action != tt.wantAction || got.Record.RecordType != tt.wantType || got.Record.Name != tt.wantName || got.Record.Content != tt.wantContent {
				t.Fatalf("unexpected decision: %+v", got)
			}
		})
	}
}
