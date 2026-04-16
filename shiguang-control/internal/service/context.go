package service

import "context"

// ctxAlias lets AccountService use the standard context.Context without
// importing it in the interface declaration (keeps the file visually small).
type ctxAlias = context.Context
