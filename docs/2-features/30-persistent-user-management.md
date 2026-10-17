# Persistent users and self-service stream-key rotation

The MediaMTX fork stores internal-authentication users separately from the server's YAML configuration.

- On the first start without a `users.json`, existing `authInternalUsers` in `mediamtx.yml` are used.
- The first successful user add/update/delete or key rotation writes a **complete** user snapshot to `users.json` next to the loaded `mediamtx.yml`.
- From then on, `users.json` **overrides the YAML user list** during start and YAML configuration reload. Other server settings continue to come from YAML.
- User changes are applied live and do not restart streaming listeners. Active connections are not forcibly disconnected. New authentications use the updated credentials.
- The file can be located elsewhere using `MTX_AUTH_USERS_FILE` (set it to a writable absolute file path, for instance to a mounted Docker volume). Back up this file and preserve it across container recreation.
- API user list responses still redact stream keys and passwords.

**Security:** `users.json` contains passwords and stream keys in the forms configured (potentially plaintext). Restrict its filesystem access, never commit it to Git, and exclude it from public backups. The server must be able to write to its directory. A file-write error prevents the live change from being applied.

## Administrator API

The endpoints remain unchanged:

- `GET /v3/config/auth-users/list`
- `POST /v3/config/auth-users/add`
- `PATCH /v3/config/auth-users/patch/:name`
- `DELETE /v3/config/auth-users/delete/:name`

These require a MediaMTX user with the `api` permission. The separate standalone HTML tester can continue using them.

## Streamer key rotation

`POST /v3/auth/self/streamkey/rotate`

Supply HTTP Basic credentials for the streamer's **named account** and an `application/json` Content-Type (an empty `{}` body is sufficient). The endpoint does *not* require the `api` permission. It verifies the streamer's password and IP restrictions, generates a fresh random stream key, saves it, and returns it **once**:

```json
{"streamkey":"sk_..."}
```

Example (PowerShell):

```powershell
$pair = "streamer:their-password"
$auth = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($pair))
Invoke-RestMethod -Uri 'http://127.0.0.1:9997/v3/auth/self/streamkey/rotate' -Method POST -Headers @{Authorization="Basic $auth"} -ContentType 'application/json' -Body '{}'
```

**Use HTTPS or a trusted local connection**: Basic credentials are sent with each request. If streamers should rotate keys over the Internet, place the API behind HTTPS with strict access controls and rate limiting. Don't expose administrator API endpoints publicly without strong authentication.

For Docker, mount the `users.json` directory persistently and configure `MTX_AUTH_USERS_FILE` to the mounted location. Avoid editing the JSON file manually while the service is running.

### Editing `users.json` manually

MediaMTX checks `users.json` once per second, including when an editor replaces
it atomically. Valid edits update authentication immediately without restarting
media listeners or disconnecting active streams. The previous credentials will
no longer authenticate new connections once the change has been applied.

Invalid JSON or users that fail MediaMTX validation are logged and **ignored**;
the currently active users remain unchanged. Correct the file and save again.
Deleting `users.json` does not automatically revert to `mediamtx.yml` while
MediaMTX is running; restore the file to resume live updates.

Avoid simultaneous manual edits and API changes: the last complete write wins.

## Emergency rotation and force-stop (optional)

For a leaked stream key, the account owner can request immediate disconnection of
**existing publishing sessions authenticated as their named account**. The new
key is saved to `users.json` and made active before those sessions are kicked.

```http
POST /v3/auth/self/streamkey/rotate
Content-Type: application/json
Authorization: Basic <streamer-name:password>

{"forceStop":true}
```

Successful responses contain `streamkey`, `disconnected` (number of publishing
sessions kicked), and `disconnectFailed` (number of sessions that could not be
kicked). **Treat `disconnectFailed > 0` as an urgent warning**: the new key is
already active and persisted, but some old sessions may still be running.

The administrator can also submit `{"streamkey":"new-key","forceStop":true}`
to `PATCH /v3/config/auth-users/patch/:name`. The flag is ignored for ordinary
changes unless it is explicitly true; force-stop in admin PATCH requires a
`streamkey` or `pass` update.

Force-stop covers RTMP/RTMPS, RTSP/RTSPS, SRT, and WebRTC publishers. It does
not disconnect readers or publishers belonging to another authenticated user.
Only sessions captured BEFORE the key change are kicked, so newly connecting
clients using the replacement key are unaffected. A session that authenticates
concurrently between that snapshot and the key change may escape the kick:
consider stopping publishing at the edge during an active incident.

**Important:** Users must authenticate to the self-service endpoint with their
account password, not their leaked stream key. A leak of both credentials
requires an administrator to reset the password too. `users.json` remains the
persistent source; active sessions are not restored from it.
