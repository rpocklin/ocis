#!/bin/sh -eu

echo "Waiting for $EOS_MGM_ALIAS:$EOS_MGM_PORT"
until nc -z -w 3 $EOS_MGM_ALIAS $EOS_MGM_PORT; do sleep 2; done;
echo "Done"