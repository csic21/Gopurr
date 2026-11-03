# Gopurr Remote Web

Authenticated browser console for Gopurr. The production bundle is embedded by
the Go binary from `internal/remote/webui`; Node is only needed to build it.

```sh
npm ci
npm run dev     # http://127.0.0.1:5173, proxies /api to Gopurr's 7899 port
npm run build   # writes ../internal/remote/webui
```

`make web` at the repository root runs the clean install and build. The Go
server still owns authentication, CSRF checks and the narrow API; the browser
never receives the kernel controller secret.
