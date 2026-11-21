// CI of digen. Jenkins (the infra repo, components/digen) runs this for every
// branch, PR and tag. There are no unit tests: examples/basic is the harness,
// so the pipeline regenerates it and fails if the committed providers_gen.go
// differs, then runs it. Nothing is published: a tag is the release (the apps
// pin digen by version in their go.mod).

pipeline {
	agent {
		docker {
			image 'golang:1.27.3-trixie'
			args '-e HOME=/tmp -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/gomod'
		}
	}

	options {
		timeout(time: 20, unit: 'MINUTES')
		buildDiscarder(logRotator(numToKeepStr: '30'))
		disableConcurrentBuilds()
	}

	stages {
		stage('Vet') {
			steps {
				sh 'go vet ./...'
			}
		}
		stage('Format') {
			steps {
				sh 'test -z "$(gofmt -l .)" || { gofmt -l .; echo "not gofmt-ed (just tidy)"; exit 1; }'
			}
		}
		stage('Example') {
			steps {
				sh 'go run ./cmd/digen -pkg ./examples/basic'
				sh 'git diff --exit-code -- examples/basic || { echo "examples/basic/providers_gen.go is stale: regenerate it (just generate-example) and commit it"; exit 1; }'
				sh 'go run ./examples/basic'
			}
		}
	}

	post {
		cleanup {
			cleanWs()
		}
	}
}
