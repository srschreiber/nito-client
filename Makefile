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

# Example bot (examples/bot/): minimal alpine worker + a hello.sh command.
# Useful for iterating on nito-bot from a local checkout without having to
# install the released binary or rebuild the distroless image on every edit.
#   make bot-example-image — build nito-example-worker:latest from examples/bot/Dockerfile
#   make run-bot-local     — build worker image + go run ./cmd/nito-bot against examples/bot/
bot-example-image:
	@echo "Building example nito-bot worker image..."
	docker build -t nito-example-worker:latest examples/bot/

run-bot-local: bot-example-image
	@echo "Running nito-bot from source against examples/bot/..."
	@# HOME is pinned to the data dir so RSA keys (engine/keys uses os.UserHomeDir)
	@# land next to bot-state.yml instead of in ~/.nito/. Matches the layout the
	@# distroless container uses, so a directory created by `make run-bot` is
	@# drop-in compatible with `make run-bot-local` and vice versa.
	NITO_BOT_DATA=$$PWD/nito-bot-data HOME=$$PWD/nito-bot-data go run ./cmd/nito-bot -f examples/bot/bot.yml -s examples/bot/scripts

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
