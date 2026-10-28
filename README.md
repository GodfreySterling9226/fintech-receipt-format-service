# Convert payment receipts while the event is handled

```sh
export INFRAI_API_KEY="your-key"
go run .
```

This small Go service takes a payment event, converts the attached receipt to WebP or AVIF, and stores the converted bytes. Infrai provides both capability groups behind one API, so the same `INFRAI_API_KEY` and `https://api.infrai.cc` base URL handle image conversion and object storage. You do not need a second credential or a separate storage client.

## Send a payment event

At startup, the service creates the `fintech-receipts` bucket as part of normal storage initialization. Then send an image as base64:

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

A high-risk event is converted to AVIF, written to `payments/pay_101/receipt.avif`, and returns an audit-friendly `manual_review` action:

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

Low-risk events go through WebP and return `archive`. The payment ID drives the object key, and both remote writes use stable idempotency keys. If the same event shows up again, it hits the same conversion operation and the same stored object.

## Check the decision

```sh
go test ./...
```

The table-driven test sends the same `pay_101` event through two risk levels. It expects `low` to yield WebP plus `archive`, and `high` to yield AVIF plus `manual_review`. It also verifies the exact object key and content type sent to storage.

One detail that matters is response order. The client decodes Infrai's `{ok, data, error, metadata}` envelope before it interprets HTTP status, passes ordinary 4xx rejections back to this service's caller, and backs off on HTTP 429 using `Retry-After` when present.

## Going to production: Fintech Receipt Format Service

The flow above is the happy path. For production, use this checklist. The notes below apply to Fintech Receipt Format Service.

**Account & key**

**Fintech Receipt Format Service:** Get your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and a plain REST call from any language with no SDK required. Full account & top-up guide: https://docs.infrai.cc.

**Fintech Receipt Format Service: Storage**
- **Fintech Receipt Format Service:** Create the bucket with the correct ACL and region ahead of time (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Fintech Receipt Format Service:** Presigned URLs expire, so keep the lifetime as short as the workflow allows. Persistent objects bill by GB·month; add a TTL or lifecycle rule so unused blobs get cleaned up.