---
name: zero-skills
description: |
 Comprehensive knowledge base for go-zero microservices framework.

 **Use this skill when:**
 - Building REST APIs with go-zero (Handler → Logic → Model architecture)
 - Creating RPC services with service discovery and load balancing
 - Implementing database operations with sqlx, MongoDB, or Redis caching
 - Adding resilience patterns (circuit breaker, rate limiting, load shedding)
 - Troubleshooting go-zero issues or understanding framework conventions
 - Generating production-ready microservices code

 **Features:**
 - Complete pattern guides with correct and incorrect examples
 - Three-layer architecture enforcement
 - Production best practices
 - Common pitfall solutions
license: MIT
allowed-tools:
 - Read
 - Grep
 - Glob
---

# go-zero Skills for AI Agents

This skill provides comprehensive go-zero microservices framework knowledge, optimized for AI agents helping developers build production-ready services. It covers REST APIs, RPC services, database operations, resilience patterns, and troubleshooting.

## When to Use This Skill

Invoke this skill when working with go-zero:
- **Creating services**: REST APIs, gRPC services, or microservices architectures
- **Database integration**: SQL, MongoDB, Redis, or connection pooling
- **Production hardening**: Circuit breakers, rate limiting, or error handling
- **Debugging**: Understanding errors, fixing configuration, or resolving issues
- **Learning**: Understanding go-zero patterns and best practices

## Knowledge Structure

### Pattern Guides (Detailed Reference)

#### 1. REST API Patterns
**Link**: [rest-api-patterns.md](references/rest-api-patterns.md)
**When to load**: Creating HTTP endpoints, implementing CRUD operations, adding middleware
**Contains**:
- Handler → Logic → Context three-layer architecture
- Request/response handling with proper types
- Middleware (auth, logging, metrics, CORS)
- Error handling with `httpx.Error()` and `httpx.OkJson()`
- Complete CRUD examples

#### 2. RPC Service Patterns
**Link**: [rpc-patterns.md](references/rpc-patterns.md)
**When to load**: Building gRPC services, service-to-service communication
**Contains**:
- Protocol Buffers definition and code generation
- Service discovery with etcd/consul/kubernetes
- Load balancing strategies
- Client configuration and interceptors
- Error handling in RPC contexts

#### 3. Database Patterns
**Link**: [database-patterns.md](references/database-patterns.md)
**When to load**: Implementing data persistence, caching, or complex queries
**Contains**:
- SQL operations with sqlx (CRUD, transactions, batch inserts)
- MongoDB integration patterns
- Redis caching strategies and cache-aside pattern
- Model generation with `goctl model`
- Connection pooling and performance tuning

#### 4. Resilience Patterns
**Link**: [resilience-patterns.md](references/resilience-patterns.md)
**When to load**: Production hardening, handling failures, managing system load
**Contains**:
- Circuit breaker configuration (Breaker)
- Rate limiting and API throttling
- Load shedding under pressure
- Timeout and retry strategies
- Graceful shutdown and degradation

#### 5. goctl Command Reference
**Link**: [references/goctl-commands.md](references/goctl-commands.md)
**When to load**: Generating code with goctl, setting up new services, post-generation steps
**Contains**:
- goctl installation and detection
- API/RPC/Model generation commands with exact flags
- Post-generation pipeline (mod tidy, import fixing, build verification)
- Config templates (API, RPC, production)
- Deployment templates (Dockerfile, Kubernetes, Docker Compose)

### Troubleshooting
**Link**: [troubleshooting/common-issues.md](troubleshooting/common-issues.md)
**When to load**: Debugging errors, configuration issues, runtime problems
**Contains**: Common error messages, solutions, configuration pitfalls, debugging tips

### Best Practices
**Link**: [best-practices/overview.md](best-practices/overview.md)
**When to load**: Production deployment, code review, optimization
**Contains**: Configuration management, logging, monitoring, security, performance

## Key Principles

When generating or reviewing go-zero code, always apply these principles:

### Always Follow

- **Three-layer separation**: Keep Handler (routing) → Logic (business) → Model (data) distinct
- **Structured errors**: Use `httpx.Error(w, err)` for HTTP errors, not `fmt.Errorf`
- **Configuration**: Load with `conf.MustLoad(&c, *configFile)` and inject via ServiceContext
- **Context propagation**: Pass `ctx context.Context` through all layers for tracing and cancellation
- **Type safety**: Define request/response types in `.api` files, generate with goctl
- **goctl generation**: Always use `goctl` to generate boilerplate, never hand-write handlers/routes

### Never Do

- Put business logic directly in handlers (violates three-layer architecture)
- Return raw errors with `w.Write()` or `fmt.Fprintf()` instead of using httpx helpers
- Hard-code configuration values (ports, hosts, database credentials)
- Skip validation of user inputs or forget to check `err != nil`
- Modify generated code (customize via `logic` layer instead)
- Bypass ServiceContext injection (leads to tight coupling and testing issues)

## Common Workflows

### Creating a New REST API Service
1. Define API specification in `.api` file with types and routes
2. Generate code: `goctl api go -api user.api -dir .`
3. Implement business logic in `internal/logic/` layer
4. Add validation and error handling with `httpx`
5. Test endpoints with proper request/response handling

### Implementing Database Operations
1. Design database schema and create tables
2. Generate model: `goctl model mysql datasource -url="..." -table="users" -dir="./model"`
3. Inject model into ServiceContext in `internal/svc/service_context.go`
4. Use sqlx methods in logic layer (`Insert`, `FindOne`, `Update`, `Delete`)
5. Handle transactions and errors properly with `ctx` propagation

### Building an RPC Service
1. Define service in `.proto` file with messages and RPCs
2. Generate code: `goctl rpc protoc user.proto --go_out=. --go-grpc_out=. --zrpc_out=.`
3. Implement service logic in `internal/logic/`
4. Configure service discovery (etcd/consul/kubernetes)
5. Test with RPC client and handle errors

## Integration with ai-context

This skill is part of a two-layer ecosystem for AI-assisted go-zero development:

| Tool | Purpose | Best For |
|------|---------|----------|
| **[ai-context](https://github.com/zeromicro/ai-context)** | Concise workflow instructions (~5KB) | GitHub Copilot, Cursor, Windsurf |
| **zero-skills** (this repo) | Comprehensive knowledge base (~45KB) | All AI tools, deep learning, reference |

The AI runs `goctl` directly in the terminal for code generation — no separate MCP server needed.

## Additional Resources

- **Official docs**: [go-zero.dev](https://go-zero.dev) - Latest API reference and guides
- **GitHub**: [zeromicro/go-zero](https://github.com/zeromicro/go-zero) - Source code and examples
- **ai-context**: [00-instructions.md](../ai-context/00-instructions.md) - Quick workflow reference

## Version Compatibility

- **Target version**: go-zero 1.5+
- **Go version**: Go 1.19 or later recommended
- **Updates**: Patterns updated regularly to reflect framework evolution

---

**Quick invocation**: Ask "How do I [task] with go-zero?"
**Need help?** Reference the specific pattern guide for detailed examples and explanations.
