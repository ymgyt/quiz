
push:
	docker build -t quiz:latest .
	docker tag quiz:latest docker.io/ymgyt/quiz:latest
	docker push docker.io/ymgyt/quiz:latest

.PHONY: push