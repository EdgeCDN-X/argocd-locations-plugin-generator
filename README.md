# ArgoCD Plugin to retreive NodeGroup Configs for Locations

For each location a following nodeGroup is returned

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
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100   343  100   199  100   144   1526   1104 --:--:-- --:--:-- --:--:--  2638
{
  "output": {
    "parameters": [
      {
        "name": "ssd",
        "nodes": [
          {
            "name": "n1",
            "ipv4": "74.220.29.158"
          }
        ],
        "cacheConfig": {
          "name": "ssd",
          "path": "/var/cache/ssd",
          "keysZone": "100m",
          "inactive": "10080m",
          "maxSize": "4096m"
        }
      }
    ]
  }
}
```


Output is an array of NodeGroups. For these we generate an application with separate config, CacheConfig and NodeSelectors.
