# Order Management API

A Go-based order management service with REST, GraphQL, database persistence, and lightweight Prometheus-style metrics compatible with local development environments.

## Features

- Create, list, fetch, and update order status
- PostgreSQL support with in-memory fallback for local testing
- Health endpoints for liveness and readiness
- JWT authentication and authorization, request IDs, recovery, and security headers
- Lightweight metrics endpoint at `/metrics` in a Prometheus-compatible format
- GraphQL endpoint at `/graphql`
- Separate Auth, API Gateway, Order, and Product processes with GORM persistence and Consul discovery

## Run locally

The application is split into four Go processes: Auth, API Gateway, Order, and Product. Auth, Order, and Product use PostgreSQL through GORM; the API Gateway validates JWTs and routes requests. All four register with Consul when it is configured. PostgreSQL must be reachable; it can be local or hosted. Docker is optional.

1. Copy `.env.example` to `.env`. Configure the PostgreSQL settings, set `BOOTSTRAP_ADMIN_USERNAME` and a strong `BOOTSTRAP_ADMIN_PASSWORD`, and set `JWT_SECRET` to a random secret of at least 32 characters (for example, generate one with `openssl rand -base64 32`). Do not commit `.env`.
2. Install Consul and start a local agent in a terminal:
   ```cmd
   consul agent -dev -client=127.0.0.1
   ```
3. From the project root, start each process in a separate terminal. Each process loads the root `.env`; `APP_PORT` is set per terminal:
   ```cmd
   set APP_PORT=8083
   go run ./cmd/auth-service
   ```
   ```cmd
   set APP_PORT=8082
   go run ./cmd/product-service
   ```
   ```cmd
   set APP_PORT=8081
   go run .
   ```
   ```cmd
   set APP_PORT=8080
   go run ./cmd/api-gateway
   ```
4. Check the Consul UI at `http://localhost:8500/ui/` and confirm `auth-service`, `product-service`, `order-service`, and `api-gateway` are healthy.

The Product Service creates the `products` table and optionally bootstraps the example product configured by `PRODUCT_BOOTSTRAP_ID`, `PRODUCT_BOOTSTRAP_NAME`, and `PRODUCT_BOOTSTRAP_PRICE`. Each service runs GORM AutoMigrate for its own tables on startup.

## Authentication and service flow

The flow follows the sample: client → Auth service (login) → API Gateway (JWT validation) → Order Service → Product Service (price lookup), with the business services discovered through Consul. The Order Service validates JWTs too, so callers cannot bypass the Gateway and access business routes anonymously. Use the access token returned by login in the `Authorization` header for protected requests.

1. Log in to the Auth Service:
   ```http
   POST http://localhost:8083/auth/login
   Content-Type: application/json

   {"username":"<bootstrap-admin>","password":"<bootstrap-password>"}
   {"username":"admin","password":"SecureAdminPassword12345"}
   ```
   The response contains an access token valid for one hour.
2. Use the bootstrap admin token to create a customer account:
   ```http
   POST http://localhost:8083/auth/users
   Authorization: Bearer <admin-token>
   Content-Type: application/json

   {"username":"customer1","password":"<strong-password>","customerId":101}
   ```
3. Log in as that customer to get a customer-scoped JWT. Customers can access only their own orders; admins can access all orders. Only admins can create customer accounts or products.

POST http://localhost:8083/auth/login
Content-Type: application/json

Body :
{
  "username": "Sid",
  "password": "Sid0123456789"
}

4. Send order API requests through the Gateway at `http://localhost:8081`, with `Authorization: Bearer <customer-token>`.

POST http://localhost:8081/api/v1/orders

body:
{
  "customerId": 101,
  "productId": 501,
  "quantity": 2
}

The Auth Service stores bcrypt password hashes in the GORM `users` table. The first admin is created from `BOOTSTRAP_ADMIN_USERNAME` and `BOOTSTRAP_ADMIN_PASSWORD`; an existing account is not silently reset when the service restarts. Store `JWT_SECRET` in a secrets manager in production and use the same value for Auth, Gateway, Order, and Product services.

## Order API

The API base URL is `http://localhost:8080`. Its order operations are:

- `POST /api/v1/orders` — create an order with `customerId`, `productId`, and `quantity`.
- `GET /api/v1/orders/{id}` — retrieve one order.
- `GET /api/v1/orders` — list orders; optional `limit` and `offset` query parameters support pagination.
- `PUT /api/v1/orders/{id}/status` — update an order status with a JSON body such as `{"status":"CONFIRMED"}`. `PATCH` is also supported for compatibility.

Creating an order requires a product in the Product Service catalog. For example, the optional bootstrapped product has ID `501` and a sample price of `12.50`. Admins can add products using `POST http://localhost:8082/products` with an admin bearer token:

```json
{"id":502,"name":"Another product","price":8.75}
```

In development without Consul, the Order Service uses `PRODUCT_SERVICE_URL` and the Gateway uses `ORDER_SERVICE_URL`. In production, set `CONSUL_HTTP_ADDR` and each process's reachable `SERVICE_ADDRESS`; Consul registration and discovery are required. The Product Service requires a valid JWT to read a product price and an admin JWT to create products.

## GraphQL operations

The project supports GraphQL at `POST http://localhost:8080/graphql` through the API Gateway. GraphQL requests require a valid JWT, just like the REST order endpoints. In SoapUI, create a REST request with method `POST`, set the endpoint to that URL, and add these headers:

```text
Content-Type: application/json
Authorization: Bearer YOUR_ACCESS_TOKEN
```

Replace `YOUR_ACCESS_TOKEN` with the `accessToken` returned by `POST http://localhost:8083/auth/login` (without angle brackets or quotes). Use a customer token to operate on that customer's orders, or an admin token for orders across customers. The GraphQL HTTP body is JSON with a `query` property; variables can optionally be sent in a `variables` property.

### Get one order

Replace `1` with the order ID returned when you created the order:

```json
{
  "query": "query { order(id: 1) { id customerId productId quantity unitPrice totalAmount status createdAt updatedAt } }"
}
```

### List orders

This returns up to 20 orders by default. `limit` is capped at 100; `offset` skips that many matching orders. For example, this requests the first 10:

```json
{
  "query": "query { orders(limit: 10, offset: 0) { id customerId productId quantity unitPrice totalAmount status createdAt updatedAt } }"
}
```

### Create an order

The customer token must belong to the same customer ID provided in the mutation. The product ID must exist in the Product Service catalog:

```json
{
  "query": "mutation { createOrder(customerId: 101, productId: 502, quantity: 2) { id customerId productId quantity unitPrice totalAmount status createdAt updatedAt } }"
}
```

### Update an order's status

Replace `1` with the existing order ID. Valid status values are `CREATED`, `CONFIRMED`, or `CANCELLED`. The caller must own the order unless using an admin token:

```json
{
  "query": "mutation { updateOrderStatus(id: 1, status: \"CONFIRMED\") { id customerId productId quantity unitPrice totalAmount status updatedAt } }"
}
```

Successful requests return a JSON response with the selected fields under `data`, for example `data.order`, `data.orders`, `data.createOrder`, or `data.updateOrderStatus`. An invalid query or rejected operation returns GraphQL `errors`; the current HTTP handler responds with status `400` for GraphQL errors. GraphQL exposes the same order functionality as REST; it does not add product or user management operations.

### Use GraphiQL in your browser

The API Gateway serves the GraphiQL browser IDE at:

```text
http://localhost:8080/graphiql
```

Start the services as described above, then open that URL in your browser. The GraphiQL page loads its UI assets from public CDNs, so the browser needs internet access to load the interface. The API requests themselves go to the same Gateway origin at `/graphql`.

1. Log in using `POST http://localhost:8083/auth/login` and copy the returned `accessToken`.
2. Paste the token into the **JWT access token** field above the GraphiQL editor. Paste only the token value; the page adds the `Bearer` prefix.
3. Run a query or mutation from the editor with the play button. The initial editor content lists the first 10 orders.

The token field is not saved by this page. Requests are authenticated against the existing GraphQL API, so a customer token only sees or changes that customer’s own orders, while an admin token can access any customer’s orders. A missing, invalid, or expired token is rejected by the API.

## Product service resilience

The order service calls `GET {PRODUCT_SERVICE_URL}/products/{productId}` (or discovers Product Service through Consul) with a per-attempt timeout and forwards the caller's validated JWT. `PRODUCT_TIMEOUT_MS` defaults to 2000 ms when unset or non-positive. `PRODUCT_RETRIES` is the number of retries after the first attempt; it is bounded from 0 to 5 (the example uses 2). Retries use exponential delays starting at 100 ms (100 ms, 200 ms, 400 ms, and so on), and stop immediately when the request context is canceled. HTTP 429 and 5xx responses and transport failures are retried; other 4xx responses are not.

The circuit breaker opens after 5 consecutive failed product lookups, rejects calls for 15 seconds, then allows a single half-open probe. Failures return a controlled `product service unavailable` error (HTTP 502 at the REST API); timeout and cancellation causes remain available for internal error inspection.

## Security

- JWTs use HS256, expire after one hour, and are required for REST and GraphQL order operations. Customers can create/read/list/update status only for their own customer ID; an admin can access any customer's orders. Auth login and health endpoints are public.
- User passwords are stored as bcrypt hashes. Bootstrap credentials and the JWT signing secret are loaded from environment variables, not source code.
- Keep real credentials in the ignored `.env` file or a deployment secret manager. `.env.example` contains placeholders only. If a real credential was pushed to Git, rotate it; deleting it from the latest file does not erase it from Git history.
- Terminate HTTPS at a trusted reverse proxy or load balancer in production and configure it to forward traffic to the application over a protected network.

## Docker

The Compose stack runs PostgreSQL, Consul, and all four Go services. Copy `.env.example` to `.env`, set `DB_PASSWORD`, `JWT_SECRET`, `BOOTSTRAP_ADMIN_USERNAME`, and `BOOTSTRAP_ADMIN_PASSWORD`, then start it:

```bash
docker compose up --build
```

The Gateway is available at `http://localhost:8080`, Auth at `http://localhost:8083`, Product at `http://localhost:8082`, and Consul UI at `http://localhost:8500/ui/`. Stop the stack with `docker compose down`; add `-v` only if you intentionally want to delete the persisted local database volume.

## Elastic Beanstalk Procfile

The root `Procfile` starts the API Gateway as Elastic Beanstalk's `web` process. CodeBuild compiles that service to the root-level `application` executable and includes both files in the build artifact. Configure the Elastic Beanstalk environment with `APP_ENV=production`, `APP_PORT=5000`, a strong `JWT_SECRET`, `CONSUL_HTTP_ADDR`, and a reachable `SERVICE_ADDRESS`. The Auth, Order, and Product services must be deployed and reachable separately; this Procfile only starts the Gateway.

## Metrics

Each service process exposes a Prometheus-styled metrics endpoint at `/metrics` without requiring local Prometheus or Grafana. The endpoint returns text metrics like:

```text
http_requests_total{method="GET",path="/api/v1/orders",status="200"} 12
http_request_duration_seconds_sum{method="GET",path="/api/v1/orders"} 0.023123
http_errors_total 0
database_errors_total 0
```

Request counts and latency are tracked by method, route, and status. `http_errors_total` counts HTTP responses with status 400 or above; `database_errors_total` counts failed PostgreSQL operations. These in-process counters reset when the respective service restarts.

Each request is logged with its request ID, method, matched route, status, duration, and error (`-` on success). The same request ID is propagated in context and attached to trace logs as work passes through controller, service, repository, and PostgreSQL. For example:

```text
event=trace request_id="abc123" layer="controller" operation="create_order"
event=trace request_id="abc123" layer="service" operation="create_order"
event=trace request_id="abc123" layer="repository" operation="create_order"
event=trace request_id="abc123" layer="postgres" operation="insert_order"
request_id=abc123 method=POST path=/api/v1/orders status=201 duration=15ms error="-"
```

This is request-correlated application logging, not distributed tracing with spans or an OpenTelemetry backend.

## Performance and concurrency

The PostgreSQL repository uses a shared `pgxpool.Pool`; PostgreSQL statements use parameters, and each API operation issues only the queries required for that operation. The `orders.id` primary key supports single-order lookups and descending ID ordering for list requests. The schema also defines customer, status, and created-time indexes for filtering/reporting queries.

`go test ./...` includes a test that submits 100 concurrent order-create requests against the thread-safe in-memory repository and verifies all requests succeed with unique IDs. To check for data races, run `go test -race ./...` in an environment with cgo and a C compiler installed.

## Tests and CodeBuild

Run all tests locally with:

```bash
go test -v ./...
```

The verbose output lists individual test cases for JWT authentication, GraphQL order operations and customer isolation, Auth and Product input/authorization checks, database configuration validation, password-hash JSON protection, repository customer scoping, Consul registration configuration, and concurrent order creation. CodeBuild uses the same command so the test names and results appear in its build logs. Go packages containing only service entrypoints or simple wiring may still display `[no test files]`; the overall build compiles those packages, while behavior is tested in the packages they call.

## Health checks

- `GET /health/live`
- `GET /health/ready`