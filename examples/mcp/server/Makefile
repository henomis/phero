APP_NAME := server
SRC := main.go

.PHONY: build run clean

build:
	go build -o $(APP_NAME) $(SRC)

run: build
	./$(APP_NAME)

clean:
	rm -f $(APP_NAME)