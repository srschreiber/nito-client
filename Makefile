run-ui:
	@echo "Building and running nito..."
	cd ui && go build -o nito . && ./nito

# Bot (headless nito user). See BOTS.md for the full setup story.
#
# Two ways to run the bot:
#   make run-bot                — `go run` from source against examples/bot/
#                                 (everyday dev loop; what most contributors want)
#   make run-bot-docker         — wrap the bot itself in the distroless image
#                                 from botcli/Dockerfile and run interactively
#                                 (production shape; first-run wizard + verify)
#   make run-bot-docker-daemon  — same as above, detached, --restart=unless-stopped
#
# Both modes use Docker for the *script* sandbox (per-command worker
# containers); they differ in whether the bot binary itself runs on the
# host or inside a distroless container.
bot-image:
	@echo "Building nito-bot container image..."
	./scripts/generate-bot-image.sh

bot-example-image:
	@echo "Building example nito-bot worker image..."
	docker build -t nito-example-worker:latest examples/bot/

run-bot: bot-example-image
	@echo "Running nito-bot from source against examples/bot/..."
	@# HOME is pinned to the data dir so RSA keys (engine/keys uses os.UserHomeDir)
	@# land next to bot-state.yml instead of in ~/.nito/. Matches the layout the
	@# distroless container uses, so a directory populated by `run-bot-docker`
	@# is drop-in compatible with `run-bot` and vice versa.
	NITO_BOT_DATA=$$PWD/nito-bot-data HOME=$$PWD/nito-bot-data go run ./cmd/nito-bot -f examples/bot/bot.yml -s examples/bot/scripts

run-bot-docker: bot-image
	@echo "Running nito-bot wrapped in distroless image (interactive)..."
	./scripts/run-bot.sh

run-bot-docker-daemon: bot-image
	@echo "Running nito-bot wrapped in distroless image (detached)..."
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
