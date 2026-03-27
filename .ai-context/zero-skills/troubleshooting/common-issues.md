# Common Issues and Solutions

## Installation Issues

### goctl command not found

```bash
# Install goctl
go install github.com/zeromicro/go-zero/tools/goctl@latest

# Verify
goctl --version
```

### go-zero version mismatch

```bash
# Update go-zero
go get -u github.com/zeromicro/go-zero
go mod tidy
```

## Code Generation Issues

### goctl api generate fails

Check API syntax:

```api
syntax = "v1"  // Required

type Request {
    Name string `json:"name"`  // json tag required
}

// Handler must have returns
@handler GetUser
get /users/:id (GetUserRequest) returns (GetUserResponse)
```

### goctl rpc generate fails

```bash
# Install protobuf tools
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

## Runtime Issues

### Service won't start - port in use

```bash
# Find process
lsof -i :8888
kill -9 <PID>

# Or change port in config
Port: 8889
```

### Database connection fails

```go
// Correct format
DataSource: "user:password@tcp(localhost:3306)/database?parseTime=true"

// Encode special chars in password
DataSource: "user:p%40ss@tcp(localhost:3306)/database"
```

### Redis connection fails

```yaml
Redis:
  Host: localhost:6379
  Type: node
  Pass: ""  # Empty if no password
```

## API Issues

### 404 Not Found

Check route registration - ensure prefix matches:

```bash
# URL will be: /api/v1/users
@server(prefix: /api/v1)
```

### Request body not parsed

```bash
# Include Content-Type header
curl -X POST http://localhost:8888/api/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John"}'
```

### Path parameter not parsed

```go
// Use path tag, not json tag
type GetUserRequest struct {
    Id int64 `path:"id"`
}
```

## RPC Issues

### RPC service not discovered

```yaml
# Server
Name: user.rpc
Etcd:
  Hosts: ["127.0.0.1:2379"]
  Key: user.rpc

# Client must use same etcd
```

### RPC call timeout

```go
// Increase timeout
ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
defer cancel()
```

## Database Issues

### Cache key conflict

```bash
# Regenerate model with cache
goctl model mysql datasource -url="..." -table="users" -dir="./model" -c
```

### Transaction rollback not working

```go
// Return error to rollback
err := l.svcCtx.DB.TransactCtx(l.ctx, func(ctx context.Context, session sqlx.Session) error {
    _, err := session.ExecCtx(ctx, query)
    if err != nil {
        return err  // Return error, not just log
    }
    return nil
})
```

## Configuration Issues

### Config not loaded

```bash
# Use absolute path
./service -f etc/config.yaml
```

### Config validation fails

```go
// Required fields have no optional/default
type Config struct {
    rest.RestConf  // Required fields embedded
    DataSource string  // No json tag = required
}
```

## Performance Issues

### High memory usage

```go
// Goroutine should respect context
select {
case <-ticker.C:
    doWork()
case <-ctx.Done():
    return
}
```

### Slow database queries

```sql
-- Add indexes
CREATE INDEX idx_email ON users(email);
```

## Logging Issues

### Logs not showing

```yaml
Log:
  Level: info  # debug < info < error
  Mode: console  # console or file
```

```go
// Use logx, not standard log
l.Logger.Info("message")
logx.Info("message")
```

## Quick Debugging Checklist

1. Is goctl installed? `goctl --version`
2. Is syntax "v1" declared?
3. Is config file path correct?
4. Are JSON tags present?
5. Is Content-Type header set?
6. Are ports available?
7. Is etcd running (for RPC)?
8. Is Redis accessible?
9. Are database credentials correct?

For more help:
- [go-zero Documentation](https://go-zero.dev)
- [GitHub Discussions](https://github.com/zeromicro/go-zero/discussions)
