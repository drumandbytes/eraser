# Running Delistd on Kubernetes

Delistd is a single-user tool with no login, so Kubernetes is only worth it if
you already run a cluster and want it there. The shape is one pod: the Delistd
container, plus the headless Chromium container if you use `fill` or `mcp`,
sharing the pod's network. Read [docker.md](docker.md) first: the volume,
`machine-id`, passphrase and browser notes there apply unchanged.

Every release publishes the image to `ghcr.io/drumandbytes/delistd` for
`linux/amd64` and `linux/arm64`, tagged with the exact release (`v2.2.0`), the minor
line (`2.2`) and `latest`. Pin the full tag in the manifest below; `latest` moves on
every release. The package is public, so the cluster needs no pull secret.

## Manifest

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: eraser-data
spec:
  accessModes: [ReadWriteOnce]   # block storage; see the notes below
  resources:
    requests:
      storage: 1Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: delistd
spec:
  replicas: 1                    # never more: one SQLite file, one scheduler
  strategy:
    type: Recreate               # the old pod must release the volume first
  selector:
    matchLabels: {app: delistd}
  template:
    metadata:
      labels: {app: delistd}
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532           # makes the volume writable by the image's user
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: delistd
          image: ghcr.io/drumandbytes/delistd:v2.2.0
          ports:
            - {name: http, containerPort: 8080}
          env:
            - {name: TZ, value: UTC}
            # Omit this and the chrome container below if you don't use fill/mcp.
            - {name: DELISTD_CHROME_URL, value: "http://localhost:9222"}
            # The Host check accepts localhost, private IPs and local names by default. Only behind a
            # proxy that authenticates, add its hostname:
            # - {name: DELISTD_ALLOWED_HOSTS, value: "lan,local,delistd.example.com"}
          volumeMounts:
            - {name: data, mountPath: /home/nonroot/.eraser}
          readinessProbe:
            httpGet: {path: /setup, port: http, httpHeaders: [{name: Host, value: localhost}]}
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
        - name: chrome
          image: chromedp/headless-shell:158.0.8127.18@sha256:830fa77e0602cb42e5cb2cbd9237ba27b649ad69d5fb82b54713aac8b7ab4f29
          volumeMounts:
            - {name: shm, mountPath: /dev/shm}
            - {name: tmp, mountPath: /tmp}
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
            runAsUser: 0         # the stock image runs as root; see docker.md
            runAsNonRoot: false
      volumes:
        - name: data
          persistentVolumeClaim: {claimName: eraser-data}
        - name: shm              # Chromium needs more than the default 64 MB
          emptyDir: {medium: Memory, sizeLimit: 1Gi}
        - name: tmp
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: delistd
spec:
  type: ClusterIP                # never LoadBalancer or an Ingress: no login
  selector: {app: delistd}
  ports:
    - {name: http, port: 8080, targetPort: http}
```

Open the UI with a port-forward, which sends `Host: localhost` and so passes
Delistd's loopback Host check:

```bash
kubectl port-forward deploy/delistd 8080:8080   # then http://localhost:8080
```

From 2.4 the Host check also accepts private IPs and local names, so another pod
reaching the Service as `delistd` or `delistd.default.svc.cluster.local` passes
it too. A public hostname (an Ingress) is refused unless you list it in
`DELISTD_ALLOWED_HOSTS` ([docker.md](docker.md#security-keep-the-port-on-127001)); do
that only behind a proxy that authenticates, since the UI has no login.

Commands run by exec-ing the binary directly (the image has no shell):

```bash
kubectl exec -it deploy/delistd -c delistd -- /usr/local/bin/delistd init
kubectl exec deploy/delistd -c delistd -- /usr/local/bin/delistd status
kubectl exec deploy/delistd -c delistd -- /usr/local/bin/delistd fill --pending
```

## Notes

- **Don't expose it.** The web UI has no login and can send mail as you. Keep
  the Service `ClusterIP` and reach it with `port-forward`, or put it behind an
  authenticating proxy you trust. The Host check is a DNS-rebinding guard, not
  authentication ([docker.md](docker.md#security-keep-the-port-on-127001)).
- **Chromium's DevTools port is reachable by other pods.** The browser's `socat`
  listens on all interfaces of the pod, and the DevTools protocol has no
  authentication. If your cluster enforces NetworkPolicy, allow ingress to this
  pod only from where you need it (port-forward traffic isn't affected by
  NetworkPolicy). Otherwise run Chromium as its own Deployment and Service and
  accept that anything in the namespace can use it, or skip the browser.
- **Storage.** `history.db` is SQLite in WAL mode, which needs working POSIX
  file locking. Use block storage (a `ReadWriteOnce` disk). Don't put it on
  NFS or SMB.
- **Scheduling.** Turn on Settings → Automation → "Run while this app is open"
  in the web UI: with one replica the pod runs a cycle every 6 hours. A
  `CronJob` running `delistd auto --once` would need the same volume, which a
  `ReadWriteOnce` claim can't share with the running pod (unless both land on
  one node), and it can't unlock a passphrase-protected config.
- **Machine ID.** The image keeps `machine-id` in the volume, so rescheduling
  the pod to another node, or upgrading the image, keeps the encrypted
  passwords readable. Deleting the PVC loses them.
- **Upgrading.** Change the image tag and apply. `Recreate` stops the old pod
  before the new one mounts the volume.
