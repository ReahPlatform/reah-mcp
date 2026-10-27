.PHONY: lint test build

lint:
	npm run lint
	python3 scripts/validate.py
	git diff --check

test:
	npm test
	python3 -m unittest discover -s scripts -p 'test_*.py' -v

build:
	npm run build
	python3 scripts/package.py
