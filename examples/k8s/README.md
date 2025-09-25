# Kubernetes Deployment

## Quick Deploy

```bash
# Create secret with your database URL
kubectl create secret generic pgmcp-secret \
  --from-literal=database-url="postgres://user:pass@your-db-host:5432/your-db" \
  --from-literal=openai-api-key="your-openai-key"

# Deploy PGMCP
kubectl apply -f deployment.yaml

# Check status
kubectl get pods -l app=pgmcp-server

# Test (port forward)
kubectl port-forward service/pgmcp-service 8080:8080
curl http://localhost:8080/healthz
```

## Configuration

- Update `pgmcp.yourdomain.com` in the Ingress to your actual domain
- Modify resource limits based on your needs
- Add SSL/TLS configuration if needed

## Production Notes

- Use proper secrets management (not literal values)
- Configure monitoring and logging
- Set up proper ingress with SSL certificates
- Consider using HorizontalPodAutoscaler for scaling
