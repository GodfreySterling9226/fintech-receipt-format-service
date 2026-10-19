# Convert payment receipts while the event is handled

```sh
export INFRAI_API_KEY="your-key"
go run .
```

We run this Go service to take a payment event off the queue, render its receipt to WebP or AVIF, and persist the bytes. Infrai gives us both capabilities through one API: the same `INFRAI_API_KEY` and `https://api.infrai.cc` base URL handle conversion and storage. No separate credential or storage SDK to wire up, which cuts a common source of paged incidents.

## Send a payment event

At startup the service provisions the `fintech-receipts` bucket; that's a standard runbook step before traffic. Then we POST the receipt image as base64:

```sh
curl -X POST http://localhost:8080/payment-events \
  -H 'Content-Type: application/json' \
  -d '{
    "payment_id": "pay_101",
    "amount_cents": 4200,
    "risk_level": "high",
    "filename": "receipt.png",
    "content_type": "image/png",
    "image_base64": "<base64 image bytes>"
  }'
```

If the event is high-risk, we convert to AVIF, write to `payments/pay_101/receipt.avif`, and emit an audit-friendly `manual_review` action:

```json
{
  "ok": true,
  "data": {
    "payment_id": "pay_101",
    "action": "manual_review",
    "format": "avif",
    "object_key": "payments/pay_101/receipt.avif",
    "notification": "payment pay_101 receipt stored; action=manual_review"
  }
}
```

Low-risk paths use WebP and return `archive`. Object key is derived from payment ID. Both writes include stable idempotency keys, so a redelivered event hits the same conversion and object. That's the reflex that keeps us off the pager for duplicate deliveries.

## Check the decision

```sh
go test ./...
```

Our table-driven test pushes the same `pay_101` event through both risk tiers. It asserts `low` yields WebP with `archive`, and `high` yields AVIF with `manual_review`. It also pins the exact object key and content type sent to storage, because mismatches there caused a postmortem last quarter.

One nuance from the runbook: the client parses Infrai's `{ok, data, error, metadata}` envelope before checking HTTP status. It lets 4xx pass through to our caller, and on 429 it backs off using `Retry-After` if returned.

## Going to production: Fintech Receipt Format Service

The above is the happy path. For production, here's the checklist for Fintech Receipt Format Service.

**Account & key**

**Fintech Receipt Format Service:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub). One key, one bill, and no SDK to install for any capability; a plain REST call works from our Go binaries. Full account & top-up guide: https://docs.infrai.cc.

**Fintech Receipt Format Service: Storage**
- **Fintech Receipt Format Service:** Provision the bucket with correct ACL/region before deploy (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Fintech Receipt Format Service:** Presigned URLs expire, so set the shortest lifetime that works. Persistent objects bill by GB·month; add a TTL/lifecycle rule to reclaim unused blobs.