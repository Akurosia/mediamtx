# Live internal user management (fork)

MediaMTX's Control API can now manage internal authentication users without restarting or rebinding RTSP/RTMP/HLS/WebRTC/SRT services.

Enable `api: yes` and authenticate with an existing user that has the `api` permission. Protect the Control API with network restrictions / TLS, since it grants user-management access.

* `GET /v3/config/auth-users/list` - list users; passwords and stream keys are redacted.
* `POST /v3/config/auth-users/add` - add a user, sending the standard `authInternalUsers` user object.
* `PATCH /v3/config/auth-users/patch/:name` - update only supplied fields (`user`, `pass`, `streamkey`, `ips`, `permissions`). Missing fields retain their existing values; sending an empty password/key clears it.
* `DELETE /v3/config/auth-users/delete/:name` - remove a user.

The special `any` user is protected from changes by these endpoints.

Example (replace host and credentials):

```sh
curl -u admin:password -X POST http://localhost:9997/v3/config/auth-users/add \
  -H 'Content-Type: application/json' \
  -d '{"user":"alice","pass":"secret","streamkey":"alicekey","permissions":[{"action":"publish","path":"live/alice"}]}'

curl -u admin:password -X PATCH http://localhost:9997/v3/config/auth-users/patch/alice \
  -H 'Content-Type: application/json' -d '{"streamkey":"newkey"}'

curl -u admin:password -X DELETE http://localhost:9997/v3/config/auth-users/delete/alice
```

**Persistence:** Changes are in memory only. They do not rewrite `mediamtx.yml` and are lost when the service restarts or the file configuration is reloaded. Keep `authInternalUsers` in the YAML file in sync if you want durable changes.

Existing connected sessions remain connected; new authentication requests use the updated credentials. Revoking an already connected publisher/reader requires explicitly disconnecting that session.
