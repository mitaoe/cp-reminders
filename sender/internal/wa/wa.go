package wa

import "errors"

var (
	ErrLoggedOut          = errors.New("whatsapp: logged out; re-pair with `wa pair`")
	ErrStreamReplaced     = errors.New("whatsapp: stream replaced by another connection with the same session")
	ErrClientOutdated     = errors.New("whatsapp: client outdated; update whatsmeow")
	ErrTemporaryBan       = errors.New("whatsapp: temporarily banned")
	ErrConnectFailure     = errors.New("whatsapp: connect failure")
	ErrStream             = errors.New("whatsapp: stream error")
	ErrConnectTimeout     = errors.New("whatsapp: timed out waiting for connection")
	ErrPairing            = errors.New("whatsapp: pairing failed")
	ErrPasskeyUnsupported = errors.New("whatsapp: passkey pairing is not supported; use QR or --phone")
)
