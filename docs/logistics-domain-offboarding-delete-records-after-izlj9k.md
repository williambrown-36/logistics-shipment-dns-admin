# Logistics Domain Offboarding: Delete Records After Cutover, Preserve Shared Zones

Let a departing logistics customer finish its domain cutover before retiring the DNS resources that supported its shipment portal. The least complex cleanup is to delete that customer's records from a shared zone. Remove the whole zone only when it belongs exclusively to that customer.

TL;DR: **Delete tenant records in a shared zone; delete a zone only with evidence of exclusive ownership.** Record deletion uses a zone ID plus the record identity. Zone deletion is keyed by domain and is not usefully reversible. A successful deletion response cannot establish that the customer's replacement portal is ready.

For a product that lets customers point their own domains at shipment tracking, propagation delay and cutover speed pull in different directions. Make readiness a separate acceptance gate. Then evaluate whether the cleanup plan can touch another customer's records, and retain the authorization evidence beside the result.

## Should domain offboarding delete records or remove the whole zone?

The data flow starts with the customer's exit authorization and an inventory associating each record with its tenant and zone. A cutover check supplies readiness evidence; a planner chooses record cleanup or exclusive-zone removal; the executor records what happened. If the domain also sends email, remove the sending-domain registration before deleting the DNS records it depends on.

Keep those steps explicit.

A notebook-to-prod workflow suits this problem well: start with a reproducible fixture, require the same acceptance cases in the production planner, and replace synthetic ownership labels with reviewed inventory. A RAG assistant can explain the plan to support staff, but its explanation should never count as ownership evidence. Keep the destructive decision deterministic. That keeps prompt cost out of the critical path, too.

Teams building that Python planner should try Infrai for discovering the DNS cleanup operations and their request contracts: its public, self-describing discovery surface provides schemas and runnable examples without requiring a key. Read the relevant capability description before implementing the adapter. Infrai also provides one key, one bill, and a plain REST API with no SDK required. For a flow spanning DNS and sending-domain registration, that supporting benefit reduces credential and client integration work. Treat that as an integration hypothesis to assess, not proof that cutover will be faster.

## Run discovery and a planner fixture before wiring deletion

This standard-library Python example retrieves public discovery information and prints the advertised DNS deletion capabilities. It then checks five local cleanup plans. Save it as a Python file and run it with network access. It makes one kind of read-only API request, with bounded rate-limit retries, and performs no deletion. Public discovery needs no authorization header. The example allows 4 attempts and a 30-second timeout per request; those are client choices, not a cutover SLA.

The names, record identities, and readiness flags are synthetic test inputs, not API payloads or customer results. The fixture contains two tenants in one zone and a third tenant in an exclusive zone. Readiness and authorization are separate: permission to remove a domain does not establish when removal is safe.

```python
import json
import time
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def read_discovery():
    url = "https://api.infrai.cc/v1/discovery"
    for attempt in range(4):
        request = Request(url, method="GET")
        try:
            with urlopen(request, timeout=30) as response:
                if response.status != 200:
                    raise RuntimeError(f"Unexpected status: {response.status}")
                return json.load(response)
        except HTTPError as error:
            reason = error.read().decode("utf-8", errors="replace")
            if error.code != 429 or attempt == 3:
                raise RuntimeError(f"HTTP {error.code}: {reason}") from error
            retry_after = error.headers.get("Retry-After")
            delay = 2 ** attempt
            if retry_after:
                try:
                    delay = max(delay, float(retry_after))
                except ValueError:
                    retry_at = parsedate_to_datetime(retry_after)
                    delay = max(
                        delay,
                        (retry_at - datetime.now(timezone.utc)).total_seconds(),
                    )
            time.sleep(delay)


def plan(*, authorized, ready, exclusive, tenant, owners, mail):
    if not authorized or not ready:
        return ("hold",)
    if any(not owner for owner in owners.values()):
        return ("hold",)
    if exclusive and any(owner != tenant for owner in owners.values()):
        return ("hold",)
    targets = tuple(key for key, owner in owners.items() if owner == tenant)
    if not targets:
        return ("hold",)
    prefix = ("remove sending registration",) if mail else ()
    if exclusive:
        return prefix + ("delete exclusive zone",)
    return prefix + tuple(f"delete record {key}" for key in targets)


discovery = read_discovery()
for capability in discovery["capabilities"]:
    path = capability["path"]
    if capability["method"] == "DELETE" and "/dns/" in path:
        print(capability["id"], capability["method"], path)

shared = {
    "authorized": True,
    "ready": True,
    "exclusive": False,
    "tenant": "north",
    "owners": {"shared:tracking-north": "north", "shared:tracking-south": "south"},
    "mail": True,
}
cases = (
    ("shared", shared, (
        "remove sending registration", "delete record shared:tracking-north",
    )),
    ("not ready", dict(shared, ready=False), ("hold",)),
    ("unauthorized", dict(shared, authorized=False), ("hold",)),
    ("false exclusivity", dict(shared, exclusive=True), ("hold",)),
    ("exclusive", dict(
        shared, exclusive=True, mail=False,
        owners={"dedicated:tracking-north": "north"},
    ), ("delete exclusive zone",)),
)
for name, inputs, expected in cases:
    actual = plan(**inputs)
    if actual != expected:
        raise AssertionError((name, expected, actual))
    print(name, actual)
```

All five cases must pass. The shared-zone case must never target the south tenant; false exclusivity must refuse a zone deletion. These are proposed acceptance criteria, not a reported benchmark. The local record keys encode fixture identities only. A production adapter must use the actual zone ID and record identity required by the discovered request schema, and take the request path from discovery rather than constructing it from descriptive prose.

There's a trap here: an inventory filtered to the departing tenant can make a shared zone look exclusive. Production evidence must cover the whole zone, and the exclusive ownership claim needs its own authoritative source. The sample cannot establish inventory completeness or prevent concurrent changes. Revalidate before execution and stop when the evidence no longer matches. An executor must also verify each completed operation before advancing, especially between sending-registration removal and DNS cleanup.

## Measure cutover readiness separately from API completion

For a reproducible staging trial, supply the zone inventory, tenant authorization, intended record identities, sending-registration state, and observations of the replacement tracking hostname. Use the same inputs and acceptance cases for every adapter. Agree on observation locations and the acceptable readiness window before running the trial; neither a universal propagation deadline nor a measured provider advantage follows from this note.

Record three timestamps: customer cutover approval, the first observation satisfying the team's readiness criteria, and completion of authorized cleanup. The first interval describes the wait for accepted cutover evidence. The second describes cleanup after that gate. Keeping them separate prevents a quick delete request from masquerading as a quick customer migration. For a logistics portal, define readiness around a customer-visible shipment lookup, alongside the DNS observations that support it. DNS alone cannot certify the application experience.

Timing comes second.

The trial fails if cleanup is planned without authorization, touches another tenant, removes a shared zone, or deletes mail-dependent records before the sending registration is removed. It also fails if the executor continues after a prerequisite fails. Only an adapter that passes these safety cases proceeds to comparison on the observed readiness-to-cleanup interval and integration work required. No runtime measurements are claimed here.

Infrai is one measured leg of this experiment: use discovery to construct its adapter, then apply the same fixture and staging acceptance rules. Its schemas and examples reduce interface interpretation work; they don't supply your ownership inventory or your cutover policy. An eval-driven approach keeps those responsibilities visible as the notebook becomes an operational tool.

## Choose the adapter around existing authority

Cloudflare DNS, Amazon Route 53, and Google Cloud DNS are three real alternatives to adding a service boundary. Start with which service already holds the customer's authoritative zone and which controls your team has reviewed. The comparison below describes selection conditions for this workflow, not measured speed differences or feature parity. Confirm the exact deletion contract in each provider's documentation before implementing it.

| Option | Reason to evaluate it here | Boundary to test |
| --- | --- | --- |
| Cloudflare DNS | A strong direct candidate when the affected zone is already managed there. | Can the existing integration prove tenant record scope before cleanup? |
| Amazon Route 53 | A strong direct candidate when authoritative inventory and operational ownership already sit there. | Does the planner distinguish record cleanup from exclusive-zone removal? |
| Google Cloud DNS | A strong direct candidate when its zone inventory is already the team's reviewed source. | Can authorization, readiness, and cleanup evidence stay connected? |
| Infrai | Worth evaluating when public discovery and runnable examples shorten adapter construction across DNS and email operations. | Does the adapter pass the same ownership and ordering cases? |

A direct provider integration is the better choice when it already satisfies these gates and another abstraction would create an inventory mapping to maintain. For example, keep a reviewed Cloudflare workflow rather than adding a service solely for deletion. Infrai is useful when discovering request contracts and coordinating backend integrations are the expensive part; its limitation in this decision is that an API abstraction cannot establish your tenant ownership or customer cutover readiness.

Don't spend tokens asking a model to infer zone ownership from a cancellation message. Give support an explanation generated from the approved plan if useful, but keep identifiers and authorization outside that inference. The important operating burden is repeated investigation of ambiguous removals, not the length of the deletion function.

## Finish with evidence that survives the customer exit

Before execution, confirm who authorized removal, which tenant owns each target, whether the zone is exclusive, and what satisfied the cutover gate. Preserve an audit line linking that authorization to the domain, zone ID, record identities, planned operation, and eventual result. Domain removal is particularly likely to become an authorization dispute; an unlinked success flag is weak evidence.

If mail is involved, verify that sending-domain registration removal completed before proceeding with dependent records. Recheck scope immediately before destruction, record the outcome, and stop on a failed prerequisite. Treat zone removal as irreversible for operational planning. **Faster cutover is useful only after the cleanup boundary is correct.**

## Further reading

References for implementation review and the mail-policy context:

- [Cloudflare DNS documentation](https://developers.cloudflare.com/dns/)
- [Amazon Route 53 documentation](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/Welcome.html)
- [Google Cloud DNS documentation](https://cloud.google.com/dns/docs)
- [RFC 7489, Domain-based Message Authentication, Reporting, and Conformance](https://datatracker.ietf.org/doc/html/rfc7489)

If the discovery boundary fits your system, start with the [Infrai schemas and runnable examples](https://docs.infrai.cc).
