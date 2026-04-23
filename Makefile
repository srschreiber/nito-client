run-ui:
	@echo "Building and running nito..."
	cd ui && go build -o nito . && ./nito

# Bot (headless nito user). See BOTS.md for the full setup story.
#   make bot-image      — build the distroless container image
#   make run-bot        — interactive first-run (wizard + verify prompt)
#   make run-bot-daemon — detached always-on run, after bootstrap
bot-image:
	@echo "Building nito-bot container image..."
	./scripts/generate-bot-image.sh

run-bot: bot-image
	@echo "Running nito-bot (interactive)..."
	./scripts/run-bot.sh

run-bot-daemon: bot-image
	@echo "Running nito-bot (detached)..."
	DETACH=1 ./scripts/run-bot.sh

tag-and-push:
	@current=$$(cat engine/tag.txt | tr -d '\n'); \
	version=$${current#v}; \
	major=$$(echo $$version | cut -d. -f1); \
	minor=$$(echo $$version | cut -d. -f2); \
	patch=$$(echo $$version | cut -d. -f3); \
	new_tag="v$$major.$$minor.$$((patch + 1))"; \
	printf "$$new_tag" > engine/tag.txt; \
	git add engine/tag.txt; \
	git commit -m "release: $$new_tag"; \
	git tag $$new_tag; \
	git push && git push --tags; \
	echo "Tagged and pushed $$new_tag"
