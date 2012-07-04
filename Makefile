SRC := $(wildcard *.go)
TARGET := hipsterd

all: $(TARGET)

$(TARGET): $(SRC)
	go build -o $@

clean:
	$(RM) $(TARGET)

.PHONY: clean
