# ArgoCD Plugin to retrieve Location-derived parameters

This plugin reads a `Location` resource and returns different parameter payloads depending on `input.parameters.resource`.

Supported resource types:

- omitted or empty: returns deployment parameters for each node group
- `IngressClass`: returns unique ingress class names derived from node group names
- `Probe`: returns a flat list of probe targets for every node in every node group

If `resource` is set to any other value, the plugin returns `unsupported resource type` with HTTP 400.

## Default resource

When `resource` is omitted or empty, the plugin returns one deployment parameter entry per node group.

```bash
curl -X POST http://localhost:8080/api/v1/getparams.execute \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "input": {
          "parameters": {
            "namespace": "argocd",
            "name": "fra1-c1-v2"
          }
        }
      }' | jq
```

Example response:

```json
{
  "output": {
    "parameters": [
      {
        "cacheName": "ssd",
        "flavor": "cache",
        "path": "/var/cache/ssd",
        "keysZone": "100m",
        "inactive": "10080m",
        "maxSize": "4096m",
        "nodeSelector": {
          "region": "fra1"
        }
      }
    ]
  }
}
```

This output is used to generate application-specific config, cache config, and node selectors.

## IngressClass resource

Set `resource` to `IngressClass` to return unique ingress class names derived from the location node groups.

```bash
curl -X POST http://localhost:8080/api/v1/getparams.execute \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "input": {
          "parameters": {
            "namespace": "argocd",
            "name": "fra1-c1-v2",
            "resource": "IngressClass"
          }
        }
      }' | jq
```

Example response:

```json
{
  "output": {
    "parameters": [
      {
        "ingressClassName": "ssd"
      },
      {
        "ingressClassName": "nvme"
      }
    ]
  }
}
```

## Probe resource

Set `resource` to `Probe` to generate flat probe targets for all node group nodes.

```bash
curl -X POST http://localhost:8080/api/v1/getparams.execute \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "input": {
          "parameters": {
            "namespace": "argocd",
            "name": "fra1-c1-v2",
            "resource": "Probe"
          }
        }
      }' | jq
```

Example response:

```json
{
  "output": {
    "parameters": [
      {
        "nodegroupName": "ssd",
        "flavor": "cache",
        "nodeName": "fra1-c1-n1",
        "address": "74.220.29.158/healthz"
      },
      {
        "nodegroupName": "ssd",
        "flavor": "cache",
        "nodeName": "fra1-c1-n1",
        "address": "2600:3c03::f03c:95ff:fe00:1/healthz"
      },
      {
        "nodegroupName": "nvme",
        "flavor": "cache",
        "nodeName": "fra1-c1-n2",
        "address": "74.220.29.159/healthz"
      }
    ]
  }
}
```
