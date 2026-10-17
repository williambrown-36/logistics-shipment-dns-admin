package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/logistics-dns-admin/internal/infrai"
	"example.com/logistics-dns-admin/internal/logistics"
)

type server struct {
	client *infrai.Client
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	s := &server{client: infrai.NewClient(key)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/dns/records", s.listRecords)
	mux.HandleFunc("POST /admin/shipment-events", s.applyShipmentEvent)
	mux.HandleFunc("DELETE /admin/dns/records/{recordID}", s.deleteRecord)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	httpServer := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("logistics DNS admin listening on %s", addr)
	log.Fatal(httpServer.ListenAndServe())
}

func (s *server) listRecords(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}
	records, err := s.client.ListRecords(r.Context(), domain)
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *server) applyShipmentEvent(w http.ResponseWriter, r *http.Request) {
	var event logistics.ShipmentEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid shipment event"})
		return
	}
	decision, err := logistics.DecideRecord(event)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	var record infrai.Record
	if decision.Action == "update" {
		record, err = s.client.UpdateRecord(r.Context(), decision.Domain, decision.ChangeID, decision.Record)
	} else {
		record, err = s.client.CreateRecord(r.Context(), decision.Domain, decision.ChangeID, decision.Record)
	}
	if err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "record": record})
}

func (s *server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	changeID := r.Header.Get("Idempotency-Key")
	if domain == "" || changeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain and Idempotency-Key are required"})
		return
	}
	if err := s.client.DeleteRecord(r.Context(), domain, r.PathValue("recordID"), changeID); err != nil {
		writeClientError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeClientError(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "dns request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
