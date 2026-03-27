# Best Practices

This directory contains production best practices for go-zero:

## Contents

- `overview.md` - General best practices overview

## Quick Reference

### Configuration
- Use environment variables for secrets
- Set reasonable timeouts
- Configure proper log levels

### Security
- Validate all input
- Use JWT for authentication
- Never log sensitive data

### Performance
- Use connection pooling
- Implement caching
- Add database indexes

### Reliability
- Add circuit breakers
- Implement rate limiting
- Use graceful shutdown
