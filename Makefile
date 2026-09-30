.ONESHELL:

test: test-pkg test-hotmic test-action test-director

test-pkg:
	go test -v $(shell go list ./pkg/... | grep -v hotmic)

test-hotmic:
	go test -v ./pkg/hotmic/...

test-director:
	cd ./cmd/director && go test ./...

test-action:
	cd action && go test -v

clean:
	rm -rf build
	mkdir build

actor:
	cd cmd/actor && go build -o ../../build/actor .

dialogue:
	cd cmd/dialogue && go install .

director:
	cd cmd/director && go install .

director-cuda:
	cd cmd/director && go install .

arduino-actor:
	cd cmd/actor && GOARCH=arm64 GOOS=linux go build -o ../../build/actor_arm64

flash-arduino-actor: arduino-actor
	# TODO: stop the running service on the Arduino before flashing
	adb push ./build/actor_arm64 /home/arduino/actor

flash-arduino-scripts:
	adb push ./scripts/gemmai.md /home/arduino/scripts/gemmai.md
	adb push ./scripts/llama3000.md /home/arduino/scripts/llama3000.md
	adb push ./scripts/phineas.md /home/arduino/scripts/phineas.md
	adb push ./scripts/qwentin.md /home/arduino/scripts/qwentin.md
	adb push ./scripts/movement.md /home/arduino/scripts/movement.md
	adb push ./scripts/answers.md /home/arduino/scripts/answers.md

build-action:
	cd action && tinygo build -target=arduino-uno-q -tags feetech -ldflags="-X main.PersonalityColor=blue" ../build/action

flash-action:
	cd action && tinygo flash -target=arduino-uno-q -tags feetech -ldflags="-X main.PersonalityColor=blue" .

mqtt:
	docker run -d --network host eclipse-mosquitto

show:
	tmuxinator s talkingheads -p ./show.yml

deploy-actors:
	./tools/deploy_actor_ssh.sh ./cmd/actor/ actor ~/Development/talkingheads/scripts/ 192.168.1.54
	./tools/deploy_actor_ssh.sh ./cmd/actor/ actor ~/Development/talkingheads/scripts/ 192.168.1.110
	./tools/deploy_actor_ssh.sh ./cmd/actor/ actor ~/Development/talkingheads/scripts/ 192.168.1.233

deploy-action:
	./tools/flash_arduino_ssh.sh ./action/ action green 192.168.1.54
	./tools/flash_arduino_ssh.sh ./action/ action blue 192.168.1.110
	./tools/flash_arduino_ssh.sh ./action/ action purple 192.168.1.233
