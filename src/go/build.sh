#!/bin/bash
work="badwolf/exec"
export GOPATH="`pwd`"

echo "start building ${work}"

ls -1 src/${work} | while read row ; do
	echo -n "building ${row} ....."

	GOOS=linux GOARCH=amd64 go install ${work}/${row}
#	GOOS=windows GOARCH=amd64 go install ${work}/${row}
#	GOOS=darwin GOARCH=amd64 go install ${work}/${row}

	if [ "$?" -ne 0 ]; then
		echo "faied"
		exit 1
	fi
	echo "done"

done
exit 0
