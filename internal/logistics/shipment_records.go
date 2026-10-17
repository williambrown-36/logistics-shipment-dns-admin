package logistics

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"example.com/logistics-dns-admin/internal/infrai"
)

type ShipmentStatus string

const (
	StatusInTransit ShipmentStatus = "in_transit"
	StatusDelivered ShipmentStatus = "delivered"
	StatusException ShipmentStatus = "exception"
)

type ProofOfDelivery struct {
	ObjectKey string `json:"object_key"`
	SHA256    string `json:"sha256"`
}

type DeliveryException struct {
	Code string `json:"code"`
	Note string `json:"note"`
}

type ShipmentEvent struct {
	EventID          string             `json:"event_id"`
	ShipmentID       string             `json:"shipment_id"`
	Domain           string             `json:"domain"`
	Status           ShipmentStatus     `json:"status"`
	TrackingHost     string             `json:"tracking_host,omitempty"`
	Proof            *ProofOfDelivery   `json:"proof,omitempty"`
	Exception        *DeliveryException `json:"exception,omitempty"`
	ExistingRecordID string             `json:"existing_record_id,omitempty"`
}

type Decision struct {
	Action   string
	Domain   string
	ChangeID string
	Record   infrai.RecordInput
}

var safeLabel = regexp.MustCompile(`[^a-z0-9-]+`)

func DecideRecord(event ShipmentEvent) (Decision, error) {
	if event.EventID == "" || event.ShipmentID == "" || event.Domain == "" {
		return Decision{}, errors.New("event_id, shipment_id, and domain are required")
	}
	label := strings.Trim(safeLabel.ReplaceAllString(strings.ToLower(event.ShipmentID), "-"), "-")
	if label == "" {
		return Decision{}, errors.New("shipment_id must contain a DNS-safe character")
	}

	record := infrai.RecordInput{RecordID: event.ExistingRecordID, TTL: 60}
	switch event.Status {
	case StatusInTransit:
		if event.TrackingHost == "" {
			return Decision{}, errors.New("tracking_host is required for an in_transit event")
		}
		record.RecordType = "CNAME"
		record.Name = "track-" + label
		record.Content = event.TrackingHost
	case StatusDelivered:
		if event.Proof == nil || event.Proof.ObjectKey == "" || event.Proof.SHA256 == "" {
			return Decision{}, errors.New("proof object_key and sha256 are required for a delivered event")
		}
		record.RecordType = "TXT"
		record.Name = "_pod." + label
		record.Content = fmt.Sprintf("object=%s;sha256=%s", event.Proof.ObjectKey, event.Proof.SHA256)
	case StatusException:
		if event.Exception == nil || event.Exception.Code == "" {
			return Decision{}, errors.New("exception code is required for an exception event")
		}
		record.RecordType = "TXT"
		record.Name = "_exception." + label
		record.Content = fmt.Sprintf("code=%s;note=%s", event.Exception.Code, event.Exception.Note)
	default:
		return Decision{}, fmt.Errorf("unsupported shipment status %q", event.Status)
	}

	action := "create"
	if event.ExistingRecordID != "" {
		action = "update"
	}
	return Decision{Action: action, Domain: event.Domain, ChangeID: event.EventID, Record: record}, nil
}
