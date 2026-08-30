up:
	kubectl apply -f deploy/k8s/namespace.yaml
	kubectl apply -f -R deploy/k8s/
	helm repo add traefik https://traefik.github.io/charts
	helm repo update
	helm upgrade --install traefik traefik/traefik \
		--namespace traefik --create-namespace \
		--version 41.2.0 \
		-f deploy/helm/traefik-values.yaml