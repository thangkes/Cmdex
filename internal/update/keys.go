package update

// Public keys trusted to sign releases (see TrustedKeys and tools/sign).
// Changing them makes installed versions refuse releases signed otherwise:
// add a key, never replace the one in use.
const (
	releaseKey = "DkAwsTjKvP961aDwdNT3HDQ5PUkxIOqxPD/P/Z1/gr0=" // the release key, on the maintainer's computer
	backupKey  = ""                                             // the backup key, kept offline
)
