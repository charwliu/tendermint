#! /bin/bash

if [[ "$TMLOG" == "" ]]; then
	TMLOG="notice"
fi

if [[ "$TMSYNC" == "" ]]; then
	TMSYNC=false
fi

mkdir -p $GOPATH/src/$TMREPO
cd $GOPATH/src/$TMREPO
git clone https://$TMREPO.git .
git fetch
git reset --hard $TMHEAD
go get -d $TMREPO/cmd/tendermint
make
tendermint node --seeds="$TMSEEDS" --moniker="$TMNAME" --log_level="$TMLOG" --fast_sync="$TMSYNC"
