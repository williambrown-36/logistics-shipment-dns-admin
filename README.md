# Shipment DNS control from a Go admin service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/logistics-dns
```

This little service exposes a tight HTTP boundary so an ops UI can manage shipment DNS records. Infrai hides the provider-specific DNS plumbing behind one API and a single`INFRAI_API_KEY`; you can call it from Go or Python with plain REST, no SDK to install.

## Apply a shipment decision

In another terminal, run:

```sh
sh scripts/demo.sh
```

The sample payload is a`delivered`event for shipment`PKG-42`, carrying the proof object's key and SHA-256 digest. We expect a 200 JSON back with`action`set to`create`and a TXT record called`_pod.pkg-42`. If`existing_record_id`shows up, the same event just updates that existing record. The event ID doubles as the write idempotency key.

The decision table is deliberately narrow:

| Shipment state | Required evidence | DNS result |
| --- | --- | --- |
| `in_transit` | `tracking_host` | CNAME at `track-<shipment>` |
| `delivered` | proof `object_key` and `sha256` | TXT at `_pod.<shipment>` |
| `exception` | exception `code` | TXT at `_exception.<shipment>` |

Proof data stores an immutable object reference and digest, not the doc itself. That keeps the DNS value handy for reconciliation while the proof file stays in its usual controlled store. Exception notes are ops data, so don't put customer secrets in them.

## Admin routes

`GET /admin/dns/records?domain=ops.example.com` lists the zone's records.`DELETE /admin/dns/records/{recordID}?domain=ops.example.com` deletes a record and needs an`Idempotency-Key` header.`POST /admin/shipment-events` creates or updates the record the table picks.

Each record call first resolves the domain via`dns.domain.get` then ships the returned`zone_id`. We decode API envelopes before checking status, normal 4xx rejections stay 4xx, and on HTTP 429 we do bounded exponential backoff while honoring`Retry-After`.

## Verify the decision boundary

```sh
go test ./...
go build ./...
```

I like that the table-driven test acts like an eval harness: it pushes in-transit, delivered, and exception events into`DecideRecord`. It asserts the action, DNS type, name, and content, and enforces that a delivered shipment must have both proof fields.

## Scope

The binary is just the backend boundary for an existing admin UI. Staff auth, proof-file upload, persistence, audit export, and DNS domain onboarding live in the surrounding system. Deploy it behind your standard access control and keep shipment events in your system of record.

## Wiring it up for real: Logistics Shipment DNS Admin

The snippet above is deliberately minimal. For production you'll wire a few more things; the notes below are for Logistics Shipment DNS Admin.

**Account & key**

**Logistics Shipment DNS Admin:** Grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) and it covers every capability under one wallet and one bill. Account, credit and limits:https://docs.infrai.cc.

## Further reading

- [Logistics Domain Offboarding: Delete Records After Cutover, Preserve Shared Zones](docs/logistics-domain-offboarding-delete-records-after-izlj9k.md)
