Branch=$(shell git symbolic-ref --short -q HEAD)
Commit=$(shell git rev-parse --short HEAD)
Date=$(shell git log --pretty=format:%cd $(Commit) -1) 
Author=$(shell git log --pretty=format:%an $(Commit) -1)
shortDate=$(shell git log -1 --format="%at" | xargs -I{} date -d @{} +%Y%m%d)
Email=$(shell git log --pretty=format:%ae $(Commit) -1)
Ver=$(shell echo $(Branch)-$(Commit)-$(shortDate))
GoVersion=$(shell go version )

.PHONY: build
build: 
	GOOS=linux go build -a -installsuffix cgo \
	-ldflags "-X 'github.com/ankr/dogesyncer/cmd.Branch=$(Branch)' \
	-X 'github.com/ankr/dogesyncer/cmd.Commit=$(Commit)' \
	-X 'github.com/ankr/dogesyncer/cmd.Date=$(Date)' \
	-X 'github.com/ankr/dogesyncer/cmd.Author=$(Author)' \
	-X 'github.com/ankr/dogesyncer/cmd.Email=$(Email)' \
	-X 'github.com/ankr/dogesyncer/cmd.GoVersion=$(GoVersion)'" -o bin/doge

.PHONY: race
race:
	go run -race main.go server --config .doge.yaml 

.PHONY: start 
start: build
	bin/doge server --config .doge.yaml 


.PHONY: docker
docker: 
	docker build \
	--build-arg GoVersion='$(GoVersion)' \
	--build-arg Branch='$(Branch)' \
	--build-arg Commit='$(Commit)' \
	--build-arg Date='$(Date)' \
	--build-arg Author='$(Author)' \
	--build-arg Email='$(Email)' \
	-t ankr/doge:$(Ver) .
	docker image prune -f --filter label=stage=builder

.PHONY: release
release: docker
	docker push ankr/doge:$(Ver)
	docker tag ankr/doge:$(Ver) ankr/doge:latest
	docker push ankr/doge:latest



