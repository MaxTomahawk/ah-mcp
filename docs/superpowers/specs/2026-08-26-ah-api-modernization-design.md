# AH API Modernization Design

## Goal

Repair the broken Albert Heijn shopping-list, favourites, basket/cart and order-detail behaviour in `ah-mcp` using API behaviour verified from current AH web traffic, while preserving the existing MCP tool names and keeping all working product, Bonus, receipt, member, store and order-history functionality intact.

## Constraints

- Only `MaxTomahawk/ah-mcp` is modified.
- Keep the existing `github.com/gwillem/appie-go v0.0.12` dependency; do not fork or replace it.
- Never commit HAR files, cookies, access tokens, refresh tokens, member data, addresses, order IDs, or other captured personal data.
- Use the existing authenticated `*appie.Client` and its `DoGraphQL` / `DoRequest` methods for AH API access.
- Preserve the 37 existing MCP tool names and compatible arguments wherever possible.
- Do not invent an order-creation/checkout mutation. The captured `order-init.har` contains reads of an already-created active order but no mutation that safely proves how to create that order.

## Verified current AH behaviour

The supplied captures show the current web client uses GraphQL basket operations:

- `basket` / `ahBasket` to read the basket.
- `basketItemsAdd(items: [BasketMutation!]!)` to add products when no active online order exists.
- `basketItemsUpdate(items: [BasketMutation!]!)` to change quantities; `quantity: 0` removes a product.
- Basket data distinguishes `itemsInList`, `itemsInOrder`, `notes`, and `summary`.
- `favoriteListV2(ids: [])` lists favourite lists.
- `favoriteListProductsAddV2` adds favourite products.
- `favoriteListProductsDeleteV2` deletes favourite-list items by item UUID, not product ID.

The mobile GraphQL schema used by the existing authenticated client contains the same Basket types and basket mutations, so the MCP can use the current GraphQL operations through its existing authenticated client without a second login mechanism.

## Architecture

Create a small, focused GraphQL helper module inside `tools` that exposes current basket and favourite operations through a narrow interface implemented by `*appie.Client`. Tool handlers call these helpers instead of obsolete REST list/cart paths.

Shopping-list tools operate on basket `itemsInList` and `notes`. Cart tools operate on the unified basket: if `itemsInOrder` exists, writes target the existing order path where needed; otherwise product writes use basket GraphQL and remain in `itemsInList`, matching current AH web behaviour. Tool output explicitly reports whether products are in the shopping list versus a real active online order.

Favourite-list listing moves from the obsolete `/mobile-services/lists/v3/lists` REST endpoint to `favoriteListV2(ids: [])`. Removal first resolves product IDs to favourite item UUIDs from the list and then calls `favoriteListProductsDeleteV2`.

Order-detail output repairs `total_price` by querying fulfilment totals when the dependency returns zero, rather than altering the dependency itself.

## Error handling

- GraphQL transport and GraphQL errors are returned with the MCP tool name/context.
- Removing missing products returns a clear `no matching items` error without mutating anything.
- Destructive clear operations retain explicit `confirm="yes"` guards.
- `ah_shopping_list_to_order` no longer claims it can create an online order. When no active order exists, it explains that AH has only a list basket and that choosing delivery/pickup is still required in AH.
- No checkout, delivery-slot reservation, or final order placement is automated without a captured and verified API contract.

## Testing

Add unit tests using a fake GraphQL client for:

- parsing basket products and free-text notes;
- add/update/remove basket mutation payloads;
- quantity zero removal;
- favourite list listing;
- product-ID to favourite item-UUID resolution;
- order total fallback mapping;
- empty/no-active-order states.

Add GitHub Actions CI running `go test ./...` and `go vet ./...`.

## Deployment

Add a multi-stage Dockerfile and GitHub Actions workflow that publishes `ghcr.io/maxtomahawk/ah-mcp:latest` and commit-SHA tags on pushes to `main`. The Portainer compose will consume that image, eliminating runtime `apt-get`, GitHub binary download and checksum logic. Existing `mcp-proxy`, Cloudflare Tunnel, callback port `9876`, localhost proxy port `9878`, token volume and Cloudflare configuration remain unchanged.