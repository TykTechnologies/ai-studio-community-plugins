module github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor

go 1.26.6

replace github.com/TykTechnologies/midsommar/v2 => ../../..

replace github.com/TykTechnologies/midsommar/microgateway => ../../../microgateway

require (
	github.com/TykTechnologies/midsommar/v2 v2.0.0
	github.com/gosimple/slug v1.13.1
	github.com/xeipuuv/gojsonschema v1.2.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/TykTechnologies/storage v1.5.0 // indirect
	github.com/TykTechnologies/tyk-identity-broker v1.8.0 // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.7.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/markbates/goth v1.64.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/pkoukk/tiktoken-go v0.1.6 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/simonfxr/pubsub v0.0.5 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/x-cray/logrus-prefixed-formatter v0.5.2 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/mgo.v2 v2.0.0-20190816093944-a6b53ec6cb22 // indirect
)
