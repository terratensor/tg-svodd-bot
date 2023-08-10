docker-build:
	DOCKER_BUILDKIT=1 docker --log-level=debug build --pull --build-arg BUILDKIT_INLINE_CACHE=1 \
		--target builder \
		--cache-from ${REGISTRY}/tg-svodd-bot:cache-builder \
		--tag ${REGISTRY}/tg-svodd-bot:cache-builder \
		--file ./Dockerfile .

	DOCKER_BUILDKIT=1 docker --log-level=debug build --pull --build-arg BUILDKIT_INLINE_CACHE=1 \
    	--cache-from ${REGISTRY}/tg-svodd-bot:cache-builder \
    	--cache-from ${REGISTRY}/tg-svodd-bot:cache \
    	--tag ${REGISTRY}/tg-svodd-bot:cache \
    	--tag ${REGISTRY}/tg-svodd-bot:${IMAGE_TAG} \
    	--file ./Dockerfile .

push-build-cache:
	docker push ${REGISTRY}/tg-svodd-bot:cache-builder
	docker push ${REGISTRY}/tg-svodd-bot:cache

push:
	docker push ${REGISTRY}/tg-svodd-bot:${IMAGE_TAG}

deploy:
	ssh -o StrictHostKeyChecking=no deploy@${HOST} -p ${PORT} 'docker network create --driver=overlay traefik-public || true'
	ssh -o StrictHostKeyChecking=no deploy@${HOST} -p ${PORT} 'rm -rf tg-svodd-bot_${BUILD_NUMBER} && mkdir tg-svodd-bot_${BUILD_NUMBER}'

	envsubst < docker-compose-production.yml > docker-compose-production-env.yml
	scp -o StrictHostKeyChecking=no -P ${PORT} docker-compose-production-env.yml deploy@${HOST}:tg-svodd-bot_${BUILD_NUMBER}/docker-compose.yml
	rm -f docker-compose-production-env.yml

	ssh -o StrictHostKeyChecking=no deploy@${HOST} -p ${PORT} 'mkdir tg-svodd-bot_${BUILD_NUMBER}/secrets'
	ssh -o StrictHostKeyChecking=no deploy@${HOST} -p ${PORT} 'cp .secrets_tg_svodd_bot/* tg-svodd-bot_${BUILD_NUMBER}/secrets'
	ssh -o StrictHostKeyChecking=no deploy@${HOST} -p ${PORT} 'cd tg-svodd-bot_${BUILD_NUMBER} && docker stack deploy --compose-file docker-compose.yml tg-svodd-bot --with-registry-auth --prune'
