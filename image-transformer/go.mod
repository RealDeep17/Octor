module github.com/webtor-io/image-transformer

go 1.25

require (
	github.com/bakins/logrus-middleware v0.0.0-20180426214643-ce4c6f8deb07
	github.com/joonix/log v0.0.0-20200409080653-9c1d2ceb5f1d
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646
	github.com/nickalie/go-webpbin v0.0.0-20170427122138-7e79cf5bb01e
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.9.3
	github.com/urfave/cli v1.22.17
	github.com/webtor-io/common-services v0.0.0-20241022160325-d391acd827ab
)

require (
	github.com/aws/aws-sdk-go v1.55.8 // indirect
	github.com/bakins/test-helpers v0.0.0-20141028124846-af83df64dc31 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dsnet/compress v0.0.1 // indirect
	github.com/frankban/quicktest v1.14.6 // indirect
	github.com/go-pg/migrations/v8 v8.1.0 // indirect
	github.com/go-pg/pg/v10 v10.15.0 // indirect
	github.com/go-pg/zerochecker v0.2.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/mholt/archiver v3.1.1+incompatible // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nats.go v1.48.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/nickalie/go-binwrapper v0.0.0-20190114141239-525121d43c84 // indirect
	github.com/nwaples/rardecode v1.1.0 // indirect
	github.com/pierrec/lz4 v2.5.2+incompatible // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.2 // indirect
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/redis/go-redis/v9 v9.16.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/ulikunitz/xz v0.5.7 // indirect
	github.com/vmihailenco/bufpool v0.1.11 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser v0.1.2 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/image v0.0.0-20220302094943-723b81ca9867 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/genproto v0.0.0-20230530153820-e85fd2cbaebc // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	mellium.im/sasl v0.3.2 // indirect
)

replace github.com/webtor-io/common-services => ../common-services

replace github.com/webtor-io/video-thumbnails-generator => ../video-thumbnails-generator
