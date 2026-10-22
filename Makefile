.PHONY: lint test build

lint:
	python3 scripts/validate.py
	git diff --check

test:
	python3 -m unittest discover -s scripts -p 'test_*.py' -v

build:
	python3 scripts/package.py
