#! /bin/sh
# business install 
# script name : build.sh

SELF_NAME="build.sh"
VERSION="1.0.0"
FILE_NAME="machinedetector"
BUILD_NAME="output"
CURR_DIR=`pwd`

echo "Start build go env ..."
if [ ! -d "$CURR_DIR/env/go" ]; then 
  tar  -zxvf $CURR_DIR/env/go1.3.3.src.tar.gz  -C  $CURR_DIR/env/
  cd $CURR_DIR/env/go/src/ && ./all.bash
fi

echo "Start build protobuf ..."
if [ ! -d "$CURR_DIR/env/protobuf-2.4.1" ]; then 
  tar  -zxvf $CURR_DIR/env/protobuf-2.4.1.tar.gz  -C  $CURR_DIR/env/
  cd $CURR_DIR/env/protobuf-2.4.1 
  INSTALL=$CURR_DIR/env/protobuf
  CONFIG="./configure --prefix=$INSTALL --enable-shared=no --enable-static=yes"
  CFLAGS="-fPIC" CXXFLAGS="-O2 -fPIC -DNDEBUG -g" $CONFIG
  make -j 8
  make install
  cp $CURR_DIR/env/protobuf/bin/protoc $CURR_DIR/bin/
fi

echo "Set Go Env ..."
export GOROOT=$CURR_DIR/env/go
export PATH=$GOROOT/bin:$CURR_DIR/bin/:$PATH

if [[ $GOPATH != *$CURR_DIR* ]]; then
    export GOPATH=$GOPATH:$CURR_DIR
fi
export GOPATH=$CURR_DIR
echo "$GOPATH"
echo "$GOROOT"
echo "$PATH"

echo "Start building protoc-gen-go plugin ..."
go get -u github.com/golang/protobuf/{proto,protoc-gen-go}
#make -C $CURR_DIR/src/github.com/golang/protobuf/
#go get -u github.com/golang/glog
#go get github.com/garyburd/redigo/redis
cd $CURR_DIR
#make -C $CURR_DIR/src/code.google.com/p/goprotobuf/
make -C $CURR_DIR/src/github.com/golang/protobuf/

#cd $CURR_DIR/src/machinedetector/ping && go test

echo "Start building $FILE_NAME"
cd $CURR_DIR/src/machinedetector/mdpb
$CURR_DIR/bin/protoc --go_out=. md.proto

cd $CURR_DIR/src/
#go install machinedetector/mdpb/
go install machinedetector

if [ $? -ne 0 ]; then
	echo "build failed!"
	exit 1
fi
rm -rf $CURR_DIR/$BUILD_NAME

#find ./ -regex "\..+\.svn$" -type d | xargs rm -rf
#if [ $? -ne 0 ]; then
#	echo "remove .svn dirs failed!"
#	exit 1
#fi

mkdir -p $CURR_DIR/$BUILD_NAME/bin
mkdir -p $CURR_DIR/$BUILD_NAME/conf
if [ $? -ne 0 ]; then
	echo "create dir \"$BUILD_NAME\" failed!"
	exit 1
fi

cp $CURR_DIR/bin/control    $CURR_DIR/$BUILD_NAME/bin/
cp $CURR_DIR/bin/supervise.md    $CURR_DIR/$BUILD_NAME/bin/
cp $CURR_DIR/bin/$FILE_NAME $CURR_DIR/$BUILD_NAME/bin/
cp $CURR_DIR/README         $CURR_DIR/$BUILD_NAME/bin/ 
cp $CURR_DIR/conf/md.conf  $CURR_DIR/$BUILD_NAME/conf/md.conf
chmod +x $CURR_DIR/$BUILD_NAME/bin/*

DATE=$(date +%Y%m%d)
echo "BuildDate: [${DATE}]" > $CURR_DIR/bin/BuildDate
echo "BuildDate: [${DATE}]" > $CURR_DIR/$BUILD_NAME/bin/BuildDate

echo "$BUILD_NAME has been created at dir $CURR_DIR/$BUILD_NAME/"
echo "build succeed!"
exit 0
