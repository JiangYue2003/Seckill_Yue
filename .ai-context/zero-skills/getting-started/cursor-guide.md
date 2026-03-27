# Cursor IDE Integration for go-zero

Cursor automatically reads all `.md` files in the `.cursorrules` directory.

## Setup

This project includes:
- `.cursorrules/` - Workflow instructions and quick patterns
- `.ai-context/zero-skills/` - Comprehensive knowledge base

## Usage

Cursor will automatically load the rules when you work on go-zero files.

For detailed patterns, refer to:
- `../.cursorrules/00-instructions.md` - Quick workflow reference
- `SKILL.md` - Full knowledge base

## Key Commands

```bash
# Generate API service
goctl api new <service_name> --style go_zero

# Generate from spec
goctl api go -api <file>.api -dir . --style go_zero

# Generate model
goctl model mysql datasource -url "<dsn>" -table "<table>" -dir ./model
```

## Workflow

1. Write `.api` spec
2. Run `goctl api go`
3. Implement logic in `internal/logic/`
4. Run `go mod tidy && go build`
