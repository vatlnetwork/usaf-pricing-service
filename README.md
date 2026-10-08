# USAF pricing service

An unauthenticated JSON HTTP API and browser interface for vendor programs and purchase-order pricing scenarios, backed by MongoDB. Domain validation lives in `domain/vendor_program.go` and `domain/scenario.go`; order pricing lives in `domain/order_pricing.go`.

## Configure and run

1. Start MongoDB or use an existing MongoDB deployment.
2. Fill in `config.json` with your MongoDB URI, database, and collection. The URI can contain database credentials and connection options. No credentials are required for HTTP API requests.
3. Run the service:

```sh
go run . -config config.json
```

The default address is `:8080`. The service verifies its MongoDB connection before accepting requests. It handles SIGINT/SIGTERM with graceful HTTP shutdown and database disconnect.

Configuration fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `server.address` | `:8080` | HTTP listen address |
| `mongodb.uri` | `mongodb://localhost:27017` | MongoDB connection string |
| `mongodb.database` | `usaf_pricing` | Database name |
| `mongodb.collection` | `vendor_programs` | Vendor program collection |
| `mongodb.connect_timeout_seconds` | `10` | Startup connection timeout, 1–300 seconds |
| `mongodb.operation_timeout_seconds` | `5` | Request/database operation timeout, 1–300 seconds |

Omitted fields use defaults. Unknown fields and invalid configuration fail startup. A different config file can be selected with `-config /path/to/config.json`.

## Browser interface

Open `http://localhost:8080/` to create, browse, edit, and delete vendor programs. The interface is embedded in the Go binary; no frontend build, external assets, or Node.js runtime is required.

The editor separates vendor-wide discount options, product group overrides, and product-specific overrides into labeled sections. Each option supports ordered percentage and dollar discount steps. Quote eligibility is editable at every scope, and product overrides support fixed net prices. Expiry is entered in the browser's local timezone and sent as a UTC timestamp; leave it blank for no expiry.

Save applies the entire program atomically, including vendor-name changes and removed options or overrides. Unsaved edits are retained after errors, and edits based on an outdated `updated_at` receive a conflict instead of overwriting newer data. Use **Discard changes** to reload the selected program. Deleting a program requires confirmation. The directory loads 100 programs at a time; search filters loaded programs, and **Load more programs** fetches the next page.

## Endpoints

All paths below are relative to `http://localhost:8080`. Send JSON request bodies with `Content-Type: application/json`.

| Method | Path | Action / body |
| --- | --- | --- |
| `GET` | `/` | Vendor program management interface |
| `GET` | `/test` | Interactive vendor-program and order-pricing tests |
| `POST` | `/vendor-programs/preview` | Price an unsaved program and order without persistence |
| `POST` | `/vendor-programs/{id}/price-order` | Price an order with saved scenarios; see purchase-order pricing below |
| `POST` | `/vendor-programs` | Create a program; see example below |
| `GET` | `/vendor-programs?limit=100&offset=0` | List programs ordered by MongoDB ID; limit 1–1000, offset ≥ 0 |
| `GET` | `/vendor-programs/{id}` | Read a program |
| `PUT` | `/vendor-programs/{id}` | Replace all editable fields using the create payload; optional `expected_updated_at` rejects stale edits with `409` |
| `DELETE` | `/vendor-programs/{id}` | Delete a program |
| `PUT` | `/vendor-programs/{id}/expiry` | `{"expires_at":"2030-01-01T00:00:00Z"}` sets expiry; `{"expires_at":null}` clears it |
| `PUT` | `/vendor-programs/{id}/quote-enabled` | Set vendor quote eligibility with `{"quote_enabled":true}` or `{"quote_enabled":false}` |
| `PUT` | `/vendor-programs/{id}/discount-options` | `UpdateDiscountOptions`: `{"discount_options":[...]}` replaces vendor options |
| `PATCH` | `/vendor-programs/{id}/product-overrides` | `UpsertProductOverrides`: `{"product_overrides":[...]}` adds/replaces the supplied products' overrides |
| `DELETE` | `/vendor-programs/{id}/product-overrides` | `RemoveProductOverrides`: `{"product_ids":["product-123"]}` removes selected overrides |
| `PATCH` | `/vendor-programs/{id}/product-group-overrides` | `UpsertProductGroupOverrides`: `{"product_group_overrides":[...]}` adds/replaces the supplied groups' overrides |
| `DELETE` | `/vendor-programs/{id}/product-group-overrides` | `RemoveProductGroupOverrides`: `{"group_names":["Kitchen"]}` removes selected group overrides |
| `GET` | `/vendor-programs/{id}/products/{productID}/discount-options?group_name=Kitchen` | Return effective options with product > product group > vendor precedence; `group_name` is optional |
| `POST` | `/products/dealer-prices` | Calculate prices for a JSON array of products; see batch pricing below |

Program IDs are MongoDB ObjectIDs represented as 24 hexadecimal characters. Product IDs are application strings, not MongoDB ObjectIDs; URL-encode them when using them in paths. The product lookup operates within the specified vendor program and does not require a stored product record. Supply the product's group through the optional `group_name` query parameter (URL-encoded); without it, only vendor and product options are considered.

Create example:

```sh
curl -i -X POST http://localhost:8080/vendor-programs \
  -H 'Content-Type: application/json' \
  -d '{
    "vendor": "Example Vendor",
    "discount_options": [
      {
        "name": "Standard",
        "discount_path": [{"type": "percentage", "amount": 10}]
      }
    ],
    "product_overrides": [
      {
        "product_id": "product-123",
        "discount_options": [
          {
            "name": "Product special",
            "discount_path": [{"type": "dollar_amount", "amount": 5}]
          }
        ]
      }
    ]
  }'
```

The response includes `id`, `vendor`, `discount_options`, `product_overrides`, `product_group_overrides`, `expires_at`, `created_at`, and `updated_at`. IDs and timestamps are generated by the service. Create returns `201` and a `Location` header; reads and updates return `200`; deleting a whole program returns `204` with no body. Lists and product discount lookups return JSON arrays.

Whole-program `PUT` requires a nonblank `vendor` and replaces all editable fields together, preserving `id` and `created_at`. Omitted option/override arrays clear those scopes; omitted `quote_enabled` becomes false, and omitted or null `expires_at` clears expiry. Pass the `updated_at` from the last read or save as `expected_updated_at` to detect stale drafts. The comparison uses MongoDB's millisecond timestamp precision. Validation failures and conflicts leave the stored program unchanged.

For option and override update endpoints, the named array is required; missing or `null` arrays are rejected. An empty `discount_options` array clears vendor options. Empty override or removal arrays leave their respective lists unchanged. Each supplied product or product group override replaces that product's or group's whole option list; unrelated overrides remain unchanged. An empty option list clears that override's options and allows options from lower-priority scopes to apply again. Option names must be unique within the vendor-level list, separately within each product group override, and separately within each product override. Names may be reused across scopes. Group names must be nonblank and unique within the program; PATCH adds or replaces overrides by exact group name. Existing programs and requests may omit `product_group_overrides`.

Application errors use `{"error":"message"}` with these statuses:

- `400`: malformed JSON, invalid input, or an invalid program ID.
- `404`: program does not exist, or has expired when looking up product discount options.
- `409`: another request changed/deleted the program during an update; reload and retry. A revision check prevents lost updates.
- `413`: request body exceeds 1 MiB.
- `415`: a supplied Content-Type is not `application/json`.
- `504`: database operation timed out.
- `500`: unexpected persistence/server error; details are logged server-side.

Amounts must be finite. Percentage amounts range from 0 through 100; dollar amounts must be nonnegative. Discount options resolve by exact name with precedence **product override > product group override > vendor-wide option**. The winning option replaces the entire lower-priority option, including its discount path. Product discount lookups return each name once: surviving vendor options first, surviving group options next, and product options last. An option with an empty discount path still replaces lower-priority options with that name.

The persistence layer uses the [official MongoDB Go driver](https://www.mongodb.com/docs/drivers/go/current/get-started/). MongoDB stores a private revision counter alongside each program for concurrent-update detection.

## Expiry

Programs accept an optional `expires_at` in create requests: an RFC 3339 timestamp with an explicit timezone, such as `"2030-01-01T00:00:00Z"` or `"2029-12-31T17:00:00-07:00"`. These examples identify the same instant. Date-only values are rejected; expiration is an exact instant, not the end of a calendar day. Timestamps are normalized to UTC and truncated to MongoDB's millisecond precision. Omitted or `null` expiry means no expiry, including for existing stored programs.

At or after `expires_at`, the program is unavailable for dealer pricing and product discount-option lookups, including all product and group overrides. Discount-option lookup returns `404` with `{"error":"vendor program has expired"}`. Pricing excludes expired programs before checking for duplicate vendor programs: one active program can coexist with expired programs for that vendor. When no active program remains, pricing returns `Unavailable` with the existing missing-vendor-program reason, even when no discount options were selected.

Expired programs remain visible in program reads and lists and can still be edited or deleted. Set or extend an expiry through:

```sh
curl -X PUT http://localhost:8080/vendor-programs/PROGRAM_ID/expiry \
  -H 'Content-Type: application/json' \
  -d '{"expires_at":"2030-01-01T00:00:00Z"}'
```

The `expires_at` field is required on this endpoint; use `{"expires_at":null}` to remove the expiry. Past timestamps are allowed and make the program immediately unavailable for pricing and discount-option lookup. Extending or clearing expiry restores availability without changing its options. No records are automatically deleted.

## Product group overrides

Include `product_group_overrides` in a create request, or update an existing program:

```sh
curl -X PATCH http://localhost:8080/vendor-programs/PROGRAM_ID/product-group-overrides \
  -H 'Content-Type: application/json' \
  -d '{
    "product_group_overrides": [
      {
        "group_name": "Kitchen",
        "discount_options": [
          {"name": "Standard", "discount_path": [{"type": "percentage", "amount": 20}]}
        ]
      }
    ]
  }'
```

For a product in `Kitchen`, this overrides the vendor's `Standard` option. If that product also has its own `Standard` override, the product's option wins. List its effective options with `GET /vendor-programs/PROGRAM_ID/products/product-123/discount-options?group_name=Kitchen`. In a batch pricing request, include `"groupName":"Kitchen"` on the product alongside `productId`, `vendor`, `listPrice`, and `discountOptions`.

Delete group overrides with `DELETE /vendor-programs/PROGRAM_ID/product-group-overrides` and body `{"group_names":["Kitchen"]}`. Product-specific overrides remain in place.

## Quote pricing

Vendor programs, product overrides, and product group overrides accept `quote_enabled` (boolean, default `false`). Set the vendor flag when creating a program or with `PUT /vendor-programs/PROGRAM_ID/quote-enabled` and `{"quote_enabled":true}`. Set override flags through the existing product/group override PATCH endpoints. A product supports quotes when its vendor, its exact matching group, or its exact product override has the flag enabled. A false flag at one scope does not disable another scope's true flag.

Eligible products expose one additional discount option, `{"name":"quote price","discount_path":[]}`, including alongside a fixed net price. This generated option replaces any configured option with the same name. Send a positive `quotePrice` in the dealer price request to use it:

```json
[{"productId":"product-123","groupName":"Kitchen","vendor":"Example Vendor","quotePrice":42.25}]
```

An eligible positive quote is returned unchanged and takes precedence over net prices and selected discounts. `listPrice` is not needed for an accepted quote. If quote pricing is unsupported, pricing falls back to the existing discount/net-price behavior using `listPrice` and the selected options. If that unsupported quote request has no discount selections, its result is `Unavailable` with reason `quote pricing is not supported and no discount options were provided`.

Zero, negative, or omitted `quotePrice` uses the existing pricing behavior. Selecting the generated `quote price` option without a positive quote produces an unavailable reason when no fixed net price applies. Quote prices must be finite. Expiry and product/vendor identity validation still apply.

## Batch pricing

Product overrides accept a `net_price` number, for example `{"product_id":"product-123","net_price":42.25}` in a create request or the `product_overrides` array of a PATCH request. A positive net price overrides all vendor, group, and product discounts. Discount lookup returns `[{"name":"net price","discount_path":[],"net_price":42.25}]` plus the quote option if eligible. Unless a positive quote is submitted, dealer pricing returns the fixed amount unchanged, regardless of selected discount options (including missing or repeated names). Discount options stored on that product override are ignored, including their validation. Net prices must be finite and nonnegative; zero or omitted means normal discount behavior. Upserting the product override with zero or no `net_price` clears its fixed price. Program expiry and required product-data validation still apply.

`POST /products/dealer-prices` accepts a JSON array with the fields from `domain.Product`. This endpoint uses camelCase product and response fields, matching the pricing response format. For the vendor program in the create example above:

```sh
curl -X POST http://localhost:8080/products/dealer-prices \
  -H 'Content-Type: application/json' \
  -d '[
    {
      "productId": "product-123",
      "vendor": "Example Vendor",
      "listPrice": 100,
      "discountOptions": ["Standard", "Product special"]
    },
    {
      "productId": "product-456",
      "vendor": "Example Vendor",
      "listPrice": 100,
      "discountOptions": ["xxx"]
    }
  ]'
```

Response (`200 OK`):

```json
{
  "product-123": {
    "dealerPrice": 85,
    "dealerPriceString": "$85.00",
    "reason": ""
  },
  "product-456": {
    "dealerPrice": null,
    "dealerPriceString": "Unavailable",
    "reason": "discount option xxx is missing"
  }
}
```

Without an accepted quote or positive net price override, the calculation starts with `listPrice`, applies selected options in the order supplied, and applies each option's discount path in order. Percentages reduce the current price; dollar amounts are subtracted from it. Only selected options are applied. Options are resolved from the vendor-wide options, overrides for the product's `groupName`, and overrides for that exact product, with product overrides taking highest priority. Only the winning option is applied when its name is selected. `groupName` is optional; an omitted, empty, or unmatched group name contributes no group options. Repeating an option in the selection applies it again.

The calculation stops at zero and never returns a negative price. Decimal arithmetic preserves intermediate precision; only the final price is rounded to two decimals, with half cents rounding up. For example, `$100` minus `10%` then `$5` is `$85.00`. All selected options are checked before calculation, so a missing option produces an error even if an earlier discount would have reached zero.

Vendor, group, and option names match exactly, including case and whitespace. A missing active vendor program, multiple active programs for the same vendor, a missing selected option, missing/invalid product data, or a database lookup failure produces an `Unavailable` result for the affected product. Other products still receive their results. Database error details are logged rather than exposed. Each distinct vendor is looked up once per request.

`listPrice` is required, finite, and nonnegative unless a quote is accepted. Without a submitted positive quote or positive net price override, missing or empty `discountOptions` means no discounts: the rounded list price is returned, provided an unexpired vendor program exists. All product IDs must be nonblank and unique within the batch; missing/duplicate IDs return `400` because the response is keyed by product ID. Malformed JSON, unknown fields, and incorrect JSON types also return `400`. An empty input array returns `{}`. The existing 1 MiB request limit applies.

## Tests

```sh
go test ./...
go vet ./...
```

To also run the HTTP/MongoDB integration tests against a running test server:

```sh
MONGODB_TEST_URI=mongodb://localhost:27017 go test ./...
```

Integration tests create uniquely named `usaf_pricing_test_*` databases and delete those databases afterward. They cover the full HTTP lifecycle, persisted domain updates, product and group override lifecycle and precedence, batch pricing and vendor lookup errors, expiry and renewal, failed-mutation rollback, and concurrent-write conflicts. Without `MONGODB_TEST_URI`, the integration tests are skipped.

## Purchase-order scenario pricing

Vendor programs now accept `scenarios` and `selection_policy` in both POST and whole-program PUT. Keep regular and special scenarios together in one vendor program. Each scenario has a stable, unique `id` and a descriptive `name`. Existing programs and `POST /products/dealer-prices` retain their original behavior; that endpoint does not evaluate scenarios. Use the order endpoints when eligibility, quantities, combinations, or promotions matter.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/vendor-programs/{id}/price-order` | Price an order using the saved program with this ID |
| `POST` | `/vendor-programs/preview` | Price `{ "program": <create payload>, "order": <order> }` without reading or writing the database |
| `GET` | `/test` | Browser workspace for testing saved programs, unsaved drafts, and tutorial examples |

The program editor at `/` includes a scenario form and **Test this draft**. The test page supports saved-program pagination, all seven tutorial examples, editable order context and product lines, explicit scenario selection, and temporary rule edits. Calculations always run on the server. Changing inputs marks the result stale; responses from older inputs are discarded. The examples are illustrative fixtures, not an import of verified vendor terms. No workbook rows or example programs are automatically written to MongoDB.

A program with scenarios uses those scenarios exclusively for order pricing. There is no implicit fallback to list price or the old discount-option fields if no scenario qualifies. Add an approved base scenario when regular pricing should remain available. Programs without scenarios use the existing vendor/group/product precedence in the new test page and order endpoint; each line's `discount_options` selects its legacy discounts.

Create example:

```json
{
  "vendor": "Atosa",
  "selection_policy": "lowest_price",
  "scenarios": [
    {
      "id": "warehouse-pickup",
      "name": "Warehouse pickup",
      "program": "regular",
      "role": "price",
      "method": "steps",
      "starting_price": "list",
      "price_unit": "each",
      "approved": true,
      "scope": {},
      "conditions": [
        {"field": "fulfillment", "operator": "=", "value": "pickup"}
      ],
      "adjustments": [
        {"type": "percentage", "amount": 50},
        {"type": "percentage", "amount": 5},
        {"type": "percentage", "amount": 2}
      ],
      "source": "Verified vendor terms"
    }
  ]
}
```

Then POST to `/vendor-programs/PROGRAM_ID/price-order`:

```json
{
  "at": "2026-10-08T12:00:00-06:00",
  "fulfillment": "pickup",
  "channel": "online",
  "lines": [
    {
      "line_id": "line-1",
      "product_id": "PRODUCT-ID",
      "quantity": 1,
      "price_unit": "each",
      "list_price": 1000
    }
  ]
}
```

This returns `available: true`, `total: 465.5`, line totals, the applied `scenario_ids`, and calculation explanations. Change fulfillment to `ship` and the result becomes `available: false` with a null total and an eligibility explanation. A valid order that cannot be priced returns HTTP 200; malformed or invalid configuration/order data returns HTTP 400. Pricing is all-or-nothing: unavailable orders do not expose partial totals as a complete price. Responses also include every scenario's `eligible_line_ids` and `rejections`, plus selected rules' PO/cart `instructions` and `sources`.

### Order inputs and units

An order belongs to the one vendor program addressed by the endpoint. It contains 1–100 lines, with unique nonblank `line_id` values. The same product can appear on several lines with different configurations. Each line requires `product_id`, integer `quantity` (1–1,000,000), and `price_unit` (`each`, `case`, or `section`). Quantities and prices are in that unit; the API never guesses a case pack or converts cases to pieces.

Optional line fields are `group_name`, `configuration`, `list_price`, `net_price`, `quote_price`, `quote_approved`, `attributes` (string values), and legacy `discount_options`. Missing prices are distinct from zero. A rule requiring an absent price is ineligible. A quote-based rule also requires `quote_approved: true`; this is the caller's assertion that the quote is verified, not an external approval lookup. `net_price` is the caller-supplied verified net for that price unit. A `fixed` scenario stores its own net price instead.

Order context includes `fulfillment`, `channel`, `dealer_class`, `account_id`, `order_use`, `shipment`, and string-valued `attributes`. These are supplied by the calling application; this service does not look up stock, dealer membership, purchase history, or shipment evidence. For example, require `attribute.stock_verified = true` to gate a stock-limited offer, or `line_attribute.domestic = true` to restrict eligible configurations. Missing attributes fail conditions, including `!=`.

`at` is an optional RFC 3339 instant; omitted means now. Program expiry and scenario dates are evaluated at this instant. This supports future and historical what-if tests against the **current saved rules**, not historical rule revisions. Scenario `starts_at` is inclusive; `ends_at` is exclusive. Both normalize to UTC milliseconds. For an offer valid through a calendar day, supply the next day's midnight with its correct timezone offset. The browser displays dates in its local timezone, including daylight-saving offset conversion.

### Scenario fields and methods

Defaults: `program: regular`, `role: price`, `method: steps`, `starting_price: list`, `price_unit: each`, `subtotal_basis: list`, and `combination: included_only`. `approved` defaults to false: drafts cannot affect prices until verified. `approval_evidence`, `source`, and `review_note` retain the reason/evidence. `method: review` always requires manual pricing, even if approved. This represents source rows with missing numerical terms without inventing a price.

`scope` supports `product_ids`, `group_names`, `configurations`, `excluded_product_ids`, and `excluded_group_names`. Empty positive product/group lists mean all products; populated IDs/groups are a union, then configuration and exclusions narrow the match. Values match exactly. Use separate scenarios for different product/configuration pairs with different prices.

| Method | Calculation / fields |
| --- | --- |
| `steps` | Start with `list`, `net`, `quote`, or another `rule`, then apply `adjustments` in order |
| `fixed` | Use required `fixed_price` per configured unit, then optional adjustments; zero is a valid fixed price |
| `quote` | Use the line's approved quote, then optional adjustments |
| `count` | Apply optional adjustments to the starting price, then `min(percent_per_unit × counted quantity, maximum_percent)` percent off every eligible line; requires `count_scope` |
| `bundle` | Required `fixed_price` for exact `bundle_components`, each with `product_id`, optional exact `configuration`, and `quantity`; requires `price_unit: bundle` |
| `free` | Exactly `buy_quantity + free_quantity` eligible units at the configured starting prices; credit the cheapest units, optionally limited by `free_value_limit` per free unit |
| `review` | No automatic price; return the configured review requirement |

Adjustment types are `percentage` (0–100), `dollar_amount` (deduction), `multiplier`, and `surcharge` (addition). All amounts must be finite and nonnegative. Discounts floor at zero after each step. Dollar adjustments apply per price unit. Bundle/free rules cannot contain additional adjustment steps; for free goods priced at discounted standard terms, reference an approved base rule that produces those unit prices.

Bundle components are exact product/configuration pairs measured in each units. Every required component must be present in the exact quantity. Other unrelated products can receive other scenarios; required bundle lines are priced together and cannot be divided between competing rules. Bundle totals are allocated across components in proportion to their quantities, **not** assigned as a per-product catalog net. Free-goods credits support mixed prices and quantities without expanding lines into individual units. Repeat promotion groups, substitutions, and arbitrary multiples are never inferred.

Price inputs, intermediate unit prices, and the final order total are limited to $1 trillion. Calculations use decimal rational arithmetic. The order total is rounded once to cents, half up; line totals use largest-remainder allocation so they sum to that total. `unit_price` is the rounded average for the line, which can differ from `total / quantity` by a cent due to allocation. Freight, tax, later rebates, and payment discounts are not included in the merchandise total.

### Eligibility and thresholds

All `conditions` must pass. Each is `{ "field": "...", "operator": "...", "value": "..." }`. Numeric fields accept `=`, `!=`, `>`, `>=`, `<`, `<=`; text fields accept `=` and `!=`. Values are strings, including numeric thresholds.

- Numeric: `eligible_subtotal`, `eligible_quantity`, `qualifying_quantity` (the count method's `count_scope`), `order_subtotal`, and `line_quantity`.
- Text: `fulfillment`, `channel`, `dealer_class`, `account_id`, `order_use`, `shipment`, `configuration`, `attribute.NAME`, and `line_attribute.NAME`.

`eligible_*` totals normally use the rule's `scope`. Set `qualification_scope` to count a different set of products, such as accessories that count toward an order minimum but do not receive the extra discount. `order_subtotal` uses all order lines. `subtotal_basis` explicitly selects supplied `list`, `net`, or approved `quote` prices; thresholds are evaluated before scenario adjustments. Where terms define a threshold using an already-discounted net, supply that verified net and select `net`. Missing basis prices make the rule ineligible. `count_scope` independently defines products counted for a quantity-driven percentage; the rule's own `scope` defines products receiving that percentage.

### Combinations and choosing a price

Independent rules never stack implicitly. A complete rule produces one candidate price per eligible line; bundles and free-goods promotions produce indivisible candidates for their complete eligible sets. The evaluator selects a compatible set covering every order line exactly once.

To apply an adjustment to a special net or regular discounted price, use `starting_price: rule` and `base_scenario_id`. The referenced rule must use `combination: compatible` and list the dependent rule in `compatible_scenario_ids`. Every ancestor must explicitly permit the final dependent rule, preventing an indirect bypass of a base's restrictions. Base chains must be acyclic, at most 16 rules deep, and use the same price unit. Bundles, free-goods offers, and manual-review rules cannot serve as unit-price bases. Both base and dependent rules must pass their own eligibility and approval checks. For example:

```json
[
  {"id":"special-net","name":"Verified special net","method":"fixed","fixed_price":1000,"approved":true,"combination":"compatible","compatible_scenario_ids":["pallet"]},
  {"id":"pallet","name":"70-kit adjustment","role":"adjustment","starting_price":"rule","base_scenario_id":"special-net","approved":true,"conditions":[{"field":"eligible_quantity","operator":">=","value":"70"}],"adjustments":[{"type":"percentage","amount":10}]}
]
```

`included_only` and `exclusive` prevent other rules from deriving additional discounts from that rule. They do not prevent unrelated products on the order from using different rules. Use conditions to express additional order-wide exclusions. `role: adjustment` requires a base-rule reference; `price` and `replacement` describe complete alternative prices. A replacement does not automatically beat a cheaper regular rule; use priority or explicit selection when that behavior is required.

Program-wide `selection_policy`:

- `lowest_price` (default): lowest permitted complete-order total.
- `highest_tier`: maximize the sum of each selected rule's `tier × covered quantity`, then minimize price.
- `priority`: maximize `priority × covered quantity`, then minimize price. Larger ranks win; default rank is zero.
- `require_review`: if more than one complete pricing alternative qualifies, return unavailable until the caller explicitly selects rules.

`selected_scenario_ids` restricts candidate rules. Every selected rule must be used; dependencies are included automatically and should not be selected separately for the same lines. Selection never bypasses eligibility, approval, or combination restrictions. Complex overlapping alternatives have a 20,000-state search limit; exceeding it returns a review reason instead of a partial or arbitrary answer. Programs support up to 2,000 scenarios, each with up to 50 conditions and 50 adjustment steps.

Whole-program PUT replaces scenarios along with existing editable fields. Omitting `scenarios` clears them; omitting `selection_policy` resets it to `lowest_price`. Existing focused discount/override/expiry endpoints preserve scenarios. Use `expected_updated_at` for optimistic concurrency as before. No data migration or store-interface changes are required; old MongoDB documents without these fields remain valid.
