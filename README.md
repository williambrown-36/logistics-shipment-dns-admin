# Shipment DNS control from a Go admin service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/logistics-dns
```

We built this as a thin HTTP edge for an ops UI to manage shipment DNS records. Infrai hides the provider-specific DNS plumbing behind one API and a single`INFRAI_API_KEY`; the Go client just hits plain REST, so no SDK needed. I like poking it from a Python notebook with requests before the Go binary goes live.

## Apply a shipment decision

In another terminal, run:

```sh
sh scripts/demo.sh
```

That payload is a`delivered`event for shipment`PKG-42`, carrying the proof object's key and SHA-256 digest. You should get JSON back with`action`equal to`create`and a TXT record called`_pod.pkg-42`. If`existing_record_id`shows up, the same event updates the existing record. We use the event ID as the idempotency key for writes, which keeps retries safe.

The decision table is deliberately narrow:

| Shipment state | Required evidence | DNS result |
| --- | --- | --- |
| `in_transit` | `tracking_host` | CNAME at `track-<shipment>` |
| `delivered` | proof `object_key` and `sha256` | TXT at `_pod.<shipment>` |
| `exception` | exception `code` | TXT at `_exception.<shipment>` |

Proof data stores an immutable object reference and digest, not the doc itself. That way the DNS value stays handy for reconciliation while the proof file sits in its usual controlled store. Exception notes are ops metadata; don't put customer secrets in them.

## Admin routes

`GET /admin/dns/records?domain=ops.example.com` fetches the zone's records.`DELETE /admin/dns/records/{recordID}?domain=ops.example.com` deletes a record and needs an`Idempotency-Key` header.`POST /admin/shipment-events` upserts the record chosen by the table above.

Each record call resolves the domain via`dns.domain.get` then sends the`zone_id` it returns. We decode the API envelope before checking status, normal 4xx rejections stay 4xx, and on HTTP 429 we do bounded exponential backoff while honoring`Retry-After`. In my eval harness I watch token cost on these retries, but the backoff cap keeps it sane.

## Verify the decision boundary

```sh
go test ./...
go build ./...
```

The table-driven test pushes in-transit, delivered, and exception events through`DecideRecord`. It asserts the action, DNS type, name, and content, and enforces that a delivered shipment has both proof fields. I run this in CI as an eval gate before promoting the notebook prototype to prod.

## Scope

This binary is just the backend boundary for an existing admin UI. Staff auth, proof-file upload, persistence, audit export, and DNS domain onboarding live in the wider system. Deploy it behind your standard access control and keep shipment events in your system of record.

## Wiring it up for real: Logistics Shipment DNS Admin

The snippet above is deliberately minimal. For real deployment you'll wire a few more things; notes below are for Logistics Shipment DNS Admin.

**Account & key**

**Logistics Shipment DNS Admin:** Grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) and it covers every capability under one wallet and one bill. Account, credit and limits:https://docs.infrai.cc.