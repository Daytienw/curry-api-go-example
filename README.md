# Curri API Example (Go)

A small, single-file Go example that exercises the [Curri API](https://docs.curri.com).
It mirrors the NodeJS, C#, and PHP examples in
[teamcurri/api-examples](https://github.com/teamcurri/api-examples) and performs
the same three steps in sequence:

1. **Display the authenticated user** (`currentUser`).
2. **Get a truck delivery quote** between two hardcoded sample addresses (`deliveryQuote`).
3. **Book a delivery** using the quote ID from step 2 (`bookDelivery`).

The Curri API is GraphQL at `https://api.curri.com/graphql`. This example uses
only the Go standard library (`net/http`, `encoding/json`, `encoding/base64`, `os`).

## Authentication

Curri uses HTTP Basic auth. You send your User ID and API key as a
base64-encoded `userID:apiKey` string in the `Authorization` header:

```
Authorization: Basic <base64(userID + ":" + apiKey)>
```

Credentials are read from environment variables and are never hardcoded.

## Setup & Run

You'll need Go installed and a Curri User ID + API key (request access and keys
through your Curri account; you also get a Sandbox key for test deliveries with
no real drivers or charges).

```sh
export CURRI_USER_ID="your_user_id"
export CURRI_API_KEY="your_api_key"

go run main.go
```

To try it without credentials, use the built-in mock server:

```sh
go run main.go -mock
```

Expected output (mock):

```
[mock mode] Starting local mock server — no credentials needed.
Logged in as: demo@example.com (user_MOCK123)
Quote quote_MOCKB34GCP: $84.50 (truck, 3.2 mi)
Booked delivery del_MOCK3XPWMR: status "Pending" (pending), $84.50
Track it at: https://curri.com/track/del_MOCK3XPWMR
```

## The quote-then-book flow

Booking a delivery is a two-step flow:

1. Call `deliveryQuote` with an origin, a destination, and a delivery method
   (`"truck"` here). It returns a quote `id` and a `fee`. **Fees are in USD
   cents**, so the example divides by 100 for display.
2. Pass that quote `id` into `bookDelivery` as `deliveryQuoteId` to confirm the
   delivery.

**Quotes expire 15 minutes after they're issued**, so book promptly after
quoting. If a quote has expired, fetch a fresh one and book against the new ID.

## Notes

**How this slots into a distributor's order system.** In a real integration the
quote-then-book flow is usually triggered by an order in a TMS/ERP/WMS. When a
sales order or transfer order is marked for local delivery, the system maps the
order's ship-from / ship-to addresses and line items into a `deliveryQuote`
call, optionally surfaces the returned fee to a CSR or the customer for
approval, and then calls `bookDelivery` with the quote ID to dispatch a driver.
The returned delivery `id` and `trackingUrl` get written back onto the order so
warehouse and customer-service staff can see status in their existing UI.
Because quotes expire after 15 minutes, the quote is best fetched at (or close
to) the moment of booking rather than cached on the order. For status updates,
you'd register a **webhook** so Curri can POST back as the delivery moves
through its lifecycle (e.g. driver assigned, picked up, delivered) — that
handler updates the order record and can trigger downstream actions like
customer notifications or marking the order fulfilled, instead of polling the
`delivery` query.

**Questions / gaps noticed while reading the docs.**

- The docs describe webhooks for delivery status changes conceptually, but I
  didn't find the exact event names / payload schema or how to register an
  endpoint in the public GraphQL reference — that detail would be needed to wire
  up the write-back path above.
- `deliveryQuote` and `bookDelivery` both take `manifestItems`, but it's not
  fully spelled out whether items quoted must exactly match items booked, or how
  required `manifestItems` are for a basic `"truck"` quote (the language examples
  quote without them). Confirming the required-vs-optional input shape per
  delivery method would remove some guesswork for a real integration.
```
