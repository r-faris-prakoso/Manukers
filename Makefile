BINARY  := manukers
REGION  ?= ap-northeast-1
PROFILE ?=

.PHONY: build run tidy clean

build:
	go build -o $(BINARY) .

tidy:
	go mod tidy

run: build
	./$(BINARY) --region $(REGION) $(if $(PROFILE),--profile $(PROFILE),)

clean:
	rm -f $(BINARY)
