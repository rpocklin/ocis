#!/bin/sh -eu

# create base folders for OCIS
./wait-for-mgm.sh

# EOS_OCIS_STORAGE_FOLDERS=('metadata' 'users' 'groups')

/usr/bin/eos mkdir -p /eos/dockertest/ocis/metadata
/usr/bin/eos chown -R 2:2 /eos/dockertest/ocis/metadata

# for FOLDER in "${EOS_OCIS_STORAGE_FOLDERS[@]}"
# do
#   FOLDER_NAME="${EOS_OCIS_HOME}/${FOLDER}"
#   echo "Creating in EOS: ${FOLDER_NAME}"
#   /usr/bin/eos mkdir -p ${FOLDER_NAME}
# done
