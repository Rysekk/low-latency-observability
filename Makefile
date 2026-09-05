up:
	helm repo add argo https://argoproj.github.io/argo-helm
	helm repo update

	helm upgrade --install argocd argo/argo-cd \
		--namespace argocd \
		--create-namespace \
		--version 10.4.2 \
		--set server.service.type=ClusterIP
	
	kubectl wait --for=condition=available deployment \
  		-l app.kubernetes.io/name=argocd-server \
  		-n argocd \
  		--timeout=300s
	
	kubectl apply -f deploy/argocd/root.yaml