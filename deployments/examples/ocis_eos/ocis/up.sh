#!/bin/bash -eux
docker-compose up -d

export EOS_HOME="/eos/dockertest"
export EOS_OCIS_HOME="${EOS_HOME}/reva"

export LDAP_BINDDN="cn=reva,ou=sysusers,dc=ocis,dc=test"
export LDAP_BINDPW="reva"
export EOS_LDAP_HOST="ocis.testnet:9125"
export EOS_MGM_ALIAS="mgm-master.testnet"
export EOS_MGM_PORT="1094"

export OCIS_CONTAINER_NAME="ocis"
export OCIS_CONTAINER_ID=`docker ps -aqf "name=^${OCIS_CONTAINER_NAME}$"`

## TODO: extend ocis image to add these functions before starting (and remove the need to kill / restart)
docker cp scripts/start-ldap.sh ${OCIS_CONTAINER_ID}:start-ldap.sh
docker cp scripts/restart-ldap-services.sh ${OCIS_CONTAINER_ID}:restart-ldap-services.sh

docker cp scripts/wait-for-mgm.sh ${OCIS_CONTAINER_ID}:wait-for-mgm.sh # used in script below
# docker cp scripts/provision-eos.sh ${OCIS_CONTAINER_ID}:provision-eos.sh

docker-compose exec ocis sh -c "./start-ldap.sh"
docker-compose exec ocis sh -c "./restart-ldap-services.sh"
# docker-compose exec ocis sh -c "./provision-eos.sh"
