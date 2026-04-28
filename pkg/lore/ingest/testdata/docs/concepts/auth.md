# Authentication in lore

## What is auth

Authentication in the lore library controls which callers can read and write
entries. Lore itself is library-only and does not ship an HTTP server, so
authentication is the caller's responsibility. The library surface is
deliberately unaware of principals, sessions, or tokens.

## Design choices

Callers wrap the Store interface with their own authorization logic:

```go
type authStore struct {
    inner lore.Store
    check func(ctx context.Context) error
}
```

This keeps the lore core free of auth policy and lets callers apply whatever
identity model their deployment needs (service account, OIDC, mTLS, etc.).
