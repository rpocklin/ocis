#!/bin/sh -eu

# sleep 60

LDAP_DEPENDENT_SERVICES=('glauth' 'accounts')

for SERVICE in "${LDAP_DEPENDENT_SERVICES[@]}"
do
  echo -n "Restarting ${SERVICE}..."
  ocis kill ${SERVICE}
  # sleep 10
  ocis run ${SERVICE}
done
