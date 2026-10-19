# Atlas on Kubernetes

Basic manifests for running Atlas on Kubernetes. This is **not** a Helm chart
or an Operator — Atlas is a stateless service and these are plain
Deployment/Service/Job manifests. See the
[roadmap "explicitly not building"](../../docs/roadmap.md#明确不做) list
for why there is no Operator or Helm chart.

## What is included

| File | Contents |
| --- | --- |
| `00-namespace.yaml` | `atlas` namespace |
| `01-configmap.yaml` | environment variables (store URL, Redis URL) |
| `02-postgres.yaml` | PostgreSQL 17 StatefulSet + PVC + Service |
| `03-redis.yaml` | Redis 8 Deployment + Service |
| `04-migrations.yaml` | schema migration ConfigMap + one-time Job |
| `05-atlas.yaml` | Atlas Deployment (4 ports, Prometheus scrape annotations) + ClusterIP Service |
| `06-rbac.yaml` | optional ServiceAccount + Role + RoleBinding for admin-port access (not part of the numbered deploy flow) |

## Prerequisites

- Kubernetes ≥ 1.28 (any flavor: k3s, minikube, kind, EKS, GKE, AKS)
- `kubectl` on your machine
- A container registry (the sample uses `ghcr.io/cuihairu/atlas:latest`; pin a
  SHA tag for production)
- At least 3 nodes with 1 GB RAM free (or adjust resource requests)

## Deploy

```bash
# 1. Create the namespace
kubectl apply -f 00-namespace.yaml

# 2. Create the ConfigMap (edit values first)
vim 01-configmap.yaml
kubectl apply -f 01-configmap.yaml

# 3. Deploy PostgreSQL and Redis
kubectl apply -f 02-postgres.yaml
kubectl apply -f 03-redis.yaml

# 4. Run schema migrations (one-time Job)
kubectl apply -f 04-migrations.yaml
kubectl -n atlas logs job/atlas-migrate    # watch until "Migrations complete"

# 5. Deploy Atlas
kubectl apply -f 05-atlas.yaml
kubectl -n atlas get pods                  # all pods should be Running
```

## Secrets

The ConfigMap carries no secrets by design: `ATLAS_ADMIN_API_KEY` is excluded and the
commented `env` block in `05-atlas.yaml` wires it from a Secret. Create the Secret,
then uncomment the block:

```bash
kubectl -n atlas create secret generic atlas-secrets \
  --from-literal=admin-api-key=<your-admin-api-key>
```

The database URL in `01-configmap.yaml` ships with a `CHANGEME` password. Either edit
the URL in the ConfigMap (dev clusters), or move the whole `ATLAS_DATABASE_URL` into the
same Secret and add a second `secretKeyRef` entry next to the admin key.

## Admin access from inside the cluster

`06-rbac.yaml` is optional — nothing in 00–05 depends on it. It creates an
`atlas-admin` ServiceAccount and a namespaced view Role with port-forward rights for
the `atlas-admin-operators` group. Replace the group with your own operator group or
user, then:

```bash
kubectl apply -f 06-rbac.yaml
kubectl -n atlas port-forward svc/atlas 8082:8082
```

## Expose Atlas

The sample uses a `ClusterIP` Service. To reach it from outside the cluster:

```bash
# NodePort (any Kubernetes, no cloud dependency)
kubectl -n atlas patch service atlas -p '{"spec":{"type":"NodePort"}}'
kubectl -n atlas get svc atlas            # find the NodePort numbers
```

Or in front of the `atlas` Service:

- **Ingress** — route `atlas.example.com` → 8080 (public API)
- **LoadBalancer** — cloud providers (EKS/GKE/AKS) create a cloud LB
- **Haproxy** — see `deploy/haproxy/` for a HAProxy config

## Production checklist

- [ ] Pin the image to a SHA tag (`ghcr.io/cuihairu/atlas:e3f802d`)
- [ ] Move `ATLAS_ADMIN_API_KEY` and `ATLAS_PG_PASSWORD` to a Secret (wiring shipped commented in `05-atlas.yaml`; see "Secrets" above)
- [ ] Set `type: LoadBalancer` or add an Ingress for external access
- [ ] Increase replicas and resource limits based on traffic
- [ ] Enable RBAC for `atlas-admin` if using the admin API from outside the cluster (optional manifest: `06-rbac.yaml`)
- [ ] Set `ATLAS_OTLP_ENDPOINT` for span export (see docs/api.md "Distributed Tracing")
- [ ] Configure monitoring scrape for `:8082/metrics` (scrape annotations shipped on the pod template in `05-atlas.yaml`; ServiceMonitor stacks write their own monitor)

## Update migrations

When a new migration file is added to `migrations/`:

1. Add the SQL to `04-migrations.yaml` under the `atlas-migrations` ConfigMap.
2. Bump the Job's annotation to re-run it:
   ```bash
   kubectl -n atlas annotate job atlas-migrate revision=$(date +%s)
   ```
3. Delete the old Job (so it re-creates with the updated ConfigMap):
   ```bash
   kubectl -n atlas delete job atlas-migrate --ignore-not-found
   kubectl apply -f 04-migrations.yaml
   ```

## Tear down

```bash
kubectl delete -f 00-namespace.yaml   # deletes everything in the atlas namespace
```

To also delete the persistent volume:

```bash
kubectl -n atlas delete pvc postgres-data
```

## Related

- [Docker Compose](../../deployments/docker/docker-compose.yml) — single-host setup
- [HA Compose](../../deploy/docker-compose.ha.yaml) — multi-replica with external storage
- [Dockerfile](../../deployments/docker/Dockerfile) — distroless runtime image
- [HA guide](../../docs/ha.md) — production topology
- [Topology](../../docs/topology.md) — deployment shapes
