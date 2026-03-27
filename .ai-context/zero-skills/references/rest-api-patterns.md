# REST API Patterns

## Core Architecture

### Three-Layer Pattern

go-zero REST APIs follow a strict three-layer architecture:

1. **Handler Layer** (`internal/handler/`) - HTTP concerns only
2. **Logic Layer** (`internal/logic/`) - Business logic implementation
3. **Service Context** (`internal/svc/`) - Dependency injection

```
HTTP Request → Handler → Logic → External Services/Database
                  ↓
            Service Context (dependencies)
```

## Handler Pattern

### Correct Pattern

Handlers should only handle HTTP-specific concerns:

```go
// internal/handler/userhandler.go
func CreateUserHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req types.CreateUserRequest
        if err := httpx.Parse(r, &req); err != nil {
            httpx.ErrorCtx(r.Context(), w, err)
            return
        }

        l := logic.NewCreateUserLogic(r.Context(), svcCtx)
        resp, err := l.CreateUser(&req)
        if err != nil {
            httpx.ErrorCtx(r.Context(), w, err)
        } else {
            httpx.OkJsonCtx(r.Context(), w, resp)
        }
    }
}
```

**Key Points:**
- Parse request with `httpx.Parse(r, &req)`
- Create logic instance with context
- Use `httpx.ErrorCtx` for errors (proper context propagation)
- Use `httpx.OkJsonCtx` for success responses
- No business logic in handler

### Common Mistakes

```go
// DON'T: Business logic in handler
func BadHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Database operations in handler
        user, err := svcCtx.UserModel.FindOne(ctx, id)
        // Complex validation in handler
        if user.Age < 18 { /* validation logic */ }
        // Multiple service calls in handler
        profile, _ := svcCtx.ProfileModel.FindOne(ctx, user.ProfileId)
    }
}

// DON'T: Direct error responses
httpx.Error(w, err)  // Missing context
http.Error(w, "error", 500)  // Use httpx package

// DON'T: Manual JSON marshaling
json.NewEncoder(w).Encode(resp)  // Use httpx.OkJsonCtx
```

## Logic Pattern

### Correct Pattern

All business logic belongs in the logic layer:

```go
// internal/logic/createuserlogic.go
type CreateUserLogic struct {
    logx.Logger
    ctx    context.Context
    svcCtx *svc.ServiceContext
}

func NewCreateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserLogic {
    return &CreateUserLogic{
        Logger: logx.WithContext(ctx),
        ctx:    ctx,
        svcCtx: svcCtx,
    }
}

func (l *CreateUserLogic) CreateUser(req *types.CreateUserRequest) (*types.CreateUserResponse, error) {
    // Validation
    if err := l.validateUser(req); err != nil {
        return nil, err
    }

    // Business logic
    user := &model.User{
        Name:  req.Name,
        Email: req.Email,
        Age:   req.Age,
    }

    // Database operation
    result, err := l.svcCtx.UserModel.Insert(l.ctx, user)
    if err != nil {
        l.Logger.Errorf("failed to insert user: %v", err)
        return nil, err
    }

    userId, _ := result.LastInsertId()

    return &types.CreateUserResponse{
        Id:      userId,
        Message: "User created successfully",
    }, nil
}
```

## Configuration Pattern

### Config Struct

```go
type Config struct {
    rest.RestConf  // Always embed for REST services

    DataSource string
    Cache      cache.CacheConf
    MaxFileSize int64 `json:",default=10485760"`
}
```

### YAML Configuration

```yaml
Name: user-api
Host: 0.0.0.0
Port: 8888
Timeout: 30000

DataSource: "user:pass@tcp(localhost:3306)/users?parseTime=true"

Cache:
  - Host: localhost:6379
    Type: node
```

## Service Context Pattern

```go
type ServiceContext struct {
    Config    config.Config
    UserModel model.UserModel
}

func NewServiceContext(c config.Config) *ServiceContext {
    conn := sqlx.NewMysql(c.DataSource)
    return &ServiceContext{
        Config:    c,
        UserModel: model.NewUserModel(conn, c.Cache),
    }
}
```

## Complete API Definition Example

```api
syntax = "v1"

info(
    title: "User API"
    desc: "User management API"
)

type (
    CreateUserRequest {
        Name     string `json:"name" validate:"required"`
        Email    string `json:"email" validate:"required,email"`
        Password string `json:"password" validate:"required,min=8"`
    }

    CreateUserResponse {
        Id int64 `json:"id"`
    }

    GetUserRequest {
        Id int64 `path:"id"`
    }

    GetUserResponse {
        Id    int64  `json:"id"`
        Name  string `json:"name"`
        Email string `json:"email"`
    }
)

@server(
    prefix: /api/v1
    group: user
)
service user-api {
    @handler CreateUser
    post /users (CreateUserRequest) returns (CreateUserResponse)

    @handler GetUser
    get /users/:id (GetUserRequest) returns (GetUserResponse)
}
```

## Best Practices

### DO:
- Keep handlers thin - only HTTP concerns
- Put all business logic in logic layer
- Use `httpx.ErrorCtx` and `httpx.OkJsonCtx`
- Always pass context
- Embed `rest.RestConf` in config
- Define clear request/response types
- Handle errors properly

### DON'T:
- Put business logic in handlers
- Use `httpx.Error` without context
- Ignore context in database operations
- Create global variables for dependencies
