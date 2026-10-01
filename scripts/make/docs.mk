# apptide keeps a generated README in every internal package, and CI fails when
# one is out of date. vivi has no equivalent, so this file has no counterpart
# there; it follows the same shape as the rest.
.PHONY: docs

# gomarkdoc picks files by the host's build tags, so on Linux it documented the
# !windows stub instead of the real package. Inline, not a target-specific
# export, which would leak into the go install that builds $(GOMARKDOC).

docs: $(GOMARKDOC) ## Generate package documentation with gomarkdoc
	@echo "Generating package docs..."
	@GOOS=windows $(GOMARKDOC) -e \
		--repository.url https://github.com/lucasassuncao/apptide \
		--repository.default-branch main \
		--repository.path / \
		-o '{{.Dir}}/README.md' ./internal/...
