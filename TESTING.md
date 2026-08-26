# ah-mcp Testing

This fork exposes 37 MCP tools. Automated CI verifies compilation, unit/regression tests, vetting and the Docker build. Live Albert Heijn account tests remain manual because they require a real authenticated account and can mutate account state.

## Automated verification

Run locally:

```bash
go test ./...
go vet ./...
docker build --build-arg VERSION=test -t ah-mcp:test .
```

GitHub Actions runs the same checks for pull requests and relevant pushes.

The regression tests specifically cover current basket GraphQL decoding, `basketItemsAdd`, `basketItemsUpdate`, quantity-zero removal, favourite-list GraphQL, product-ID to favourite-item UUID mapping, list item references/check state, basket mode detection and order-total fallback logic.

## Manual test rules

Use a recognisable test product and remove it again afterwards. Prefer read-only tools first. Do not test `ah_logout`, clear operations or real-order editing casually on an account with important state.

Legend:

- 🟢 read-only / low risk
- 🟡 mutation that is normally reversible
- 🔴 destructive or real-order state change

## 1. Account and server

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_server_info` | 🟢 | Call once | MCP and dependency versions returned |
| `ah_login` | 🟢 | Call while logged out, complete browser login, call again | Login URL first; connected account second |
| `ah_logout` | 🔴 | Only when intentionally testing re-login | Tokens removed; protected tools report not authenticated |
| `ah_get_member_profile` | 🟢 | Call after login | Profile fields returned |

## 2. Products, Bonus and stores

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_search_products` | 🟢 | Search `melk` | Product IDs/titles/prices returned |
| `ah_search_products_bulk` | 🟢 | Search `melk`, `brood`, `kaas` together | Separate result sets returned |
| `ah_search_products_filtered` | 🟢 | Search `kaas` with `bonus=true` | Bonus-filtered products returned |
| `ah_get_product` | 🟢 | Fetch an ID from search | Full product detail returned |
| `ah_get_products_bulk` | 🟢 | Fetch 2–3 IDs | Details for each valid ID returned |
| `ah_get_frequent_items` | 🟢 | `min_order_count=2` | Purchase-frequency results or valid empty result |
| `ah_get_bonus_offers` | 🟢 | Search/filter current offers | Current Bonus offers returned |
| `ah_get_bonus_group_products` | 🟢 | Use a segment ID from an offer | Qualifying products returned |
| `ah_search_stores` | 🟢 | Search near profile/postcode | Store IDs and addresses returned |
| `ah_get_last_chance_items` | 🟢 | Use a store ID | Bargain list or valid empty result |

## 3. Main shopping list

Start by calling `ah_get_shopping_list` and save the returned `item_id` values. Current item references are MCP-level references such as `product:<product-id>` or `note:<index>`; they are intentionally not the obsolete AH `listItemId` values.

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_shopping_list` | 🟢 | Read current list | Product items and free-text notes are returned with names, quantities and `item_id` |
| `ah_add_to_shopping_list` | 🟡 | Add 1 test product | Product appears in subsequent read |
| `ah_add_free_text_to_shopping_list` | 🟡 | Add `TESTITEM CHATGPT` | Free-text description appears, not an empty name |
| `ah_check_shopping_list_item` | 🟡 | Check then uncheck an `item_id` returned by the list read | `isStrikethrough`/checked state changes and can be reversed |
| `ah_remove_from_shopping_list` | 🟡 | Remove the test product and test note | Both disappear from subsequent read |
| `ah_clear_shopping_list` | 🔴 | Only on a deliberately disposable list; `confirm=yes` | Main shopping list becomes empty |
| `ah_shopping_list_to_order` | 🟡 | Test with products on list | If a real active order exists, products may be moved through the order path; if no active order exists, tool clearly reports that it cannot create/reserve a delivery/pickup order |

### Empty-basket regression

With no active online order:

1. Call `ah_get_cart` and confirm `mode="shopping_list"`, `has_active_order=false`.
2. Call `ah_update_cart_item` with a test product and quantity 1.
3. Call `ah_get_cart` again.
4. Verify the product is present without an `Order does not exist` failure.
5. Remove the test product again.

This is the regression for the original problem where cart mutation required manually creating an AH order first.

## 4. Favourite lists / Mijn Lijstjes

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_favorite_lists` | 🟢 | List all saved lists | `favoriteListV2` data returns list IDs/names/counts |
| `ah_add_to_favorite_list` | 🟡 | Add one test product to an existing list | Mutation reports success; product appears in list |
| `ah_remove_from_favorite_list` | 🟡 | Remove the same product by product ID | MCP resolves the AH favourite-item UUID and deletes it |

A 404 from the old `/mobile-services/lists/v3/lists` endpoint must never be required for these three tools.

## 5. Basket/cart

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_cart` | 🟢 | Read with and without active order | Returns `mode` and `has_active_order`; no false `Order does not exist` for list-only basket |
| `ah_get_cart_summary` | 🟢 | Compare with full cart | Totals/mode agree with basket state |
| `ah_update_cart_item` | 🟡 | Set test product to 1, then 2 | List-only basket uses current basket GraphQL; active order uses order path |
| `ah_remove_from_cart` | 🟡 | Remove that product | Product removed from the correct state |
| `ah_clear_cart` | 🔴 | Only with disposable product state; `confirm=yes` | Product items cleared; free-text shopping-list notes are deliberately preserved in list-only mode |

Important: a list-only basket is not a delivery/pickup order. The MCP does not claim otherwise.

## 6. Receipts

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_receipts` | 🟢 | Fetch latest 5–10 | Receipt IDs, dates and totals returned |
| `ah_get_receipt_details` | 🟢 | Fetch one returned receipt ID | Items, discounts and payments returned |

## 7. Online orders

| Tool | Risk | Test | Passing result |
|---|---:|---|---|
| `ah_get_order_history` | 🟢 | Read upcoming/open orders | Valid list or empty result |
| `ah_get_past_orders` | 🟢 | Read last 5 closed orders | Delivered orders returned |
| `ah_get_order_details` | 🟢 | Fetch an ID from history | Items returned and `total_price` uses fulfilment fallback when dependency detail total is zero |
| `ah_reopen_order` | 🔴 | Only on a genuinely modifiable upcoming order | Order becomes editable |
| `ah_update_order_items` | 🔴 | Change one controlled test product after reopen | Change request accepted |
| `ah_revert_order` | 🔴 | Always run after a reopen test | Reopened order is resubmitted/closed again |

The real-order editing sequence must be treated as one transaction:

```text
ah_reopen_order
→ ah_update_order_items
→ ah_revert_order
```

Do not stop after `ah_reopen_order`.

## 8. End-to-end safe smoke test

Recommended live smoke sequence after deployment:

1. `ah_get_server_info`
2. `ah_login`
3. `ah_get_member_profile`
4. `ah_search_products` for a harmless test product
5. `ah_get_bonus_offers`
6. `ah_get_shopping_list`
7. Add one test product to the shopping list
8. Read list and check/uncheck it using its returned `item_id`
9. Remove the same test product
10. `ah_get_favorite_lists`
11. `ah_get_cart`
12. With no active order, add one product using `ah_update_cart_item`
13. Read cart; verify `mode="shopping_list"`
14. Remove the same product
15. `ah_get_receipts`
16. `ah_get_order_history`
17. `ah_get_past_orders`
18. `ah_get_order_details` for one known order

Do not include clear/logout/reopen operations in the normal smoke test.

## Expected tool count

The current server registers **37 tools**. If an MCP client shows an older tool catalog after upgrading, refresh/reconnect the connector or start a new chat/session as required by that client before concluding the server is missing tools.
