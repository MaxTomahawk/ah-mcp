# ah-mcp — Albert Heijn MCP Server

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

A Model Context Protocol (MCP) server for Albert Heijn. This fork keeps the existing authenticated AH client and MCP tool names, while modernizing the basket, shopping-list and favourite-list flows to the current AH APIs.

## What this fork changes

- Uses current AH GraphQL basket operations for reading and changing product items.
- Uses `favoriteListV2`, `favoriteListProductsAddV2` and `favoriteListProductsDeleteV2` for named favourite lists.
- Fixes shopping-list removal/clear and free-text display.
- Fixes check/uncheck through current basket semantics; `ah_get_shopping_list` returns stable MCP item references such as `product:413639` and `note:0`.
- Makes cart tools state-aware: they report whether the account currently has only a shopping-list basket or a real active online order.
- Allows product basket mutations without requiring an already-created online order.
- Adds a fallback for missing `ah_get_order_details.total_price` values.
- Ships a Dockerfile, CI, and a GHCR image built from this repository.

The existing `github.com/gwillem/appie-go` dependency remains a normal library dependency. It is not forked or deployed as a separate service.

## Important basket/order semantics

Albert Heijn has a basket before a delivery/pickup order necessarily exists.

`ah_get_cart` therefore returns:

- `mode: "shopping_list"` and `has_active_order: false` when products are only in the shopping-list basket;
- `mode: "active_order"` and `has_active_order: true` when AH has a real online order.

When there is no active order, `ah_update_cart_item` can add/change/remove products in the shopping-list basket, but it **does not reserve a delivery slot or create/submit an online order**.

This project deliberately does not invent an unverified checkout/order-creation mutation. Selecting delivery/pickup and creating a real order remains an AH action until that API flow is captured and verified safely.

## Docker / GHCR

The recommended deployment for this fork is:

```yaml
services:
  ah-mcp:
    image: ghcr.io/maxtomahawk/ah-mcp:latest
    restart: unless-stopped
    environment:
      AH_MCP_PORT: "3000"
      AH_MCP_BASE_URL: "https://ah-mcp.example.com"
      AH_CALLBACK_HOST: "http://192.168.1.10:9876"
      AH_CALLBACK_PORT: "9876"
      AH_REMOTE: "true"
      AH_TOKENS_PATH: "/data/tokens.json"
    volumes:
      - ah_mcp_data:/data
    ports:
      - "9876:9876"

volumes:
  ah_mcp_data:
```

The image runs the MCP process as an unprivileged user. Its entrypoint fixes ownership of `/data` first so an existing named volume can be reused.

## Build from source

```bash
git clone https://github.com/MaxTomahawk/ah-mcp.git
cd ah-mcp
go test ./...
go vet ./...
go build -o ah-mcp .
```

Requires Go 1.23+.

## Run directly

```bash
./ah-mcp --transport stdio
./ah-mcp --transport sse --remote
./ah-mcp --transport streamable-http --remote
```

For a remote deployment, call `ah_login`; it returns a browser URL. Complete the AH login in the browser and call `ah_login` again to verify the session.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `AH_CALLBACK_HOST` | `http://localhost:9876` | Base URL users open during the AH OAuth login flow. |
| `AH_CALLBACK_PORT` | `9876` | Local callback listener port. |
| `AH_MCP_PORT` | `3000` | MCP HTTP listener port. |
| `AH_MCP_BASE_URL` | `http://localhost:3000` | Public base URL for remote HTTP/SSE deployments. |
| `AH_TOKENS_PATH` | XDG config path | Token file location. Use `/data/tokens.json` in Docker. |
| `AH_REMOTE` | `false` | Disable automatic local browser opening. |
| `AH_MCP_TOKEN` | unset | Optional bearer/query token for direct public MCP exposure. Prefer an authenticated reverse proxy for Internet-facing use. |
| `AH_LOG_FILE` | unset | Optional log file path. Logs also go to stderr. |

## Available tools

The server exposes 37 tools.

### Account/system

- `ah_get_server_info`
- `ah_login`
- `ah_logout`
- `ah_get_member_profile`

### Product search and offers

- `ah_search_products`
- `ah_search_products_bulk`
- `ah_search_products_filtered`
- `ah_get_product`
- `ah_get_products_bulk`
- `ah_get_frequent_items`
- `ah_get_bonus_offers`
- `ah_get_bonus_group_products`
- `ah_search_stores`
- `ah_get_last_chance_items`

### Shopping list and favourites

- `ah_get_shopping_list`
- `ah_add_free_text_to_shopping_list`
- `ah_add_to_shopping_list`
- `ah_remove_from_shopping_list`
- `ah_clear_shopping_list`
- `ah_check_shopping_list_item`
- `ah_shopping_list_to_order`
- `ah_get_favorite_lists`
- `ah_add_to_favorite_list`
- `ah_remove_from_favorite_list`

### Basket/cart

- `ah_get_cart`
- `ah_get_cart_summary`
- `ah_update_cart_item`
- `ah_remove_from_cart`
- `ah_clear_cart`

### Receipts and online orders

- `ah_get_receipts`
- `ah_get_receipt_details`
- `ah_get_order_history`
- `ah_get_past_orders`
- `ah_get_order_details`
- `ah_reopen_order`
- `ah_update_order_items`
- `ah_revert_order`

## Destructive operations

`ah_clear_cart` and `ah_clear_shopping_list` require `confirm="yes"`.

The order-editing sequence is intentionally separate from normal basket editing. If you use `ah_reopen_order`, always finish that workflow with `ah_revert_order`. Treat real-order mutations as higher risk than shopping-list basket changes.

## Testing

CI runs on pushes and pull requests:

```bash
go test ./...
go vet ./...
docker build --build-arg VERSION=ci -t ah-mcp:ci .
```

See [TESTING.md](TESTING.md) for the manual integration test sequence.

## Security

AH access/refresh tokens are stored only in the configured token file. Never commit that file, `.env` secrets, browser cookies, HAR captures, Cloudflare tokens, or MCP bearer tokens.

If the MCP is reachable from the public Internet, put authentication/rate limiting in front of it (for example Cloudflare Access) or configure `AH_MCP_TOKEN`. Do not rely on an obscure URL as authentication.

## Acknowledgements

The project uses [appie-go](https://github.com/gwillem/appie-go) as its authenticated Albert Heijn Go client. This fork keeps that dependency unchanged and implements the verified modern basket/favourite compatibility layer inside `ah-mcp` itself.
