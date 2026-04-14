run-shell:
	@echo "Running shell..."
	go run ./shellapp/

tag-and-push:
	@current=$$(cat shellapp/tag.txt | tr -d '\n'); \
	version=$${current#v}; \
	major=$$(echo $$version | cut -d. -f1); \
	minor=$$(echo $$version | cut -d. -f2); \
	patch=$$(echo $$version | cut -d. -f3); \
	new_tag="v$$major.$$minor.$$((patch + 1))"; \
	printf "$$new_tag" > shellapp/tag.txt; \
	git add shellapp/tag.txt; \
	git commit -m "release: $$new_tag"; \
	git tag $$new_tag; \
	git push && git push --tags; \
	echo "Tagged and pushed $$new_tag"
