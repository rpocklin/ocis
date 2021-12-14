#!/bin/sh -eux

# wait for EOS
./wait-for-mgm.sh

export LDAP_BINDDN=${LDAP_BINDDN:-cn=reva,ou=sysusers,dc=ocis,dc=test}
export LDAP_BINDPW=${LDAP_BINDPW:-reva}

echo "----- [ocis] LDAP setup -----";
authconfig --enableldap --enableldapauth --ldapserver=ldap://${EOS_LDAP_HOST} --ldapbasedn="dc=ocis,dc=test" --update

# try \n
sed -i "s/^binddn.*/binddn ${LDAP_BINDDN}/" /etc/nslcd.conf
sed -i "s/^bindpw.*/bindpw ${LDAP_BINDPW}/" /etc/nslcd.conf

# start in debug mode;
nslcd -d &

#nscd or nslcd
# failed to bind to LDAP server ldap://ocis.testnet:9125: Can't contact LDAP server: Transport endpoint is not connected
# nslcd: [8b4567] <group/member="nscd"> DEBUG: ldap_unbind()