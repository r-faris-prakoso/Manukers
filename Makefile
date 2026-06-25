BINARY  := manukers
REGION  ?= ap-northeast-1
PROFILE ?=

.PHONY: build run install tidy clean

build:
	go build -o $(BINARY) .

install:
	go install .

run: build
	./$(BINARY) --region $(REGION) $(if $(PROFILE),--profile $(PROFILE),)

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
