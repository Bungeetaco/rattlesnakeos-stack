package templates

const BuildTemplate = `
#!/bin/bash

if [ $# -lt 1 ]; then
  echo "Need to specify device name as argument"
  exit 1
fi

# check if supported device
DEVICE=$1
case "$DEVICE" in
  marlin|sailfish)
    DEVICE_FAMILY=marlin
    AVB_MODE=verity_only
    ;;
  taimen)
    DEVICE_FAMILY=taimen
    AVB_MODE=vbmeta_simple
    ;;
  walleye)
    DEVICE_FAMILY=muskie
    AVB_MODE=vbmeta_simple
    ;;
  crosshatch|blueline)
    DEVICE_FAMILY=crosshatch
    AVB_MODE=vbmeta_chained
    EXTRA_OTA=(--retrofit_dynamic_partitions)
    ;;
  sargo|bonito)
    DEVICE_FAMILY=bonito
    AVB_MODE=vbmeta_chained
    EXTRA_OTA=(--retrofit_dynamic_partitions)
    ;;
  *)
    echo "warning: unknown device $DEVICE, using Pixel 3 defaults"
    DEVICE_FAMILY=$1
    AVB_MODE=vbmeta_chained
    ;;
esac

# this is a build time option to override stack setting IGNORE_VERSION_CHECKS
FORCE_BUILD=false
if [ "$2" = true ]; then
  echo "Setting FORCE_BUILD=true"
  FORCE_BUILD=true
fi

# allow build and branch to be specified
AOSP_BUILD=$3
AOSP_BRANCH=$4

# set region
REGION=<% .Region %>
export AWS_DEFAULT_REGION=${REGION}

# stack name
STACK_NAME=<% .Name %>

# version of stack running
STACK_VERSION=<% .Version %>

# prevent default action of shutting down on exit
PREVENT_SHUTDOWN=<% .PreventShutdown %>

# whether version checks should be ignored
IGNORE_VERSION_CHECKS=<% .IgnoreVersionChecks %>

# whether keys are client side encrypted or not
ENCRYPTED_KEYS="<% .EncryptedKeys %>"
ENCRYPTION_KEY=
ENCRYPTION_PIPE="/tmp/key"

# pin to specific version of android
ANDROID_VERSION="10.0"

# build type (user or userdebug)
BUILD_TYPE="user"

# build channel (stable or beta)
BUILD_CHANNEL="stable"

# user customizable things
HOSTS_FILE=<% .HostsFile %>

# attestion server
ENABLE_ATTESTATION=<% .EnableAttestation %>
ATTESTATION_MAX_SPOT_PRICE=<% .AttestationMaxSpotPrice %>

# aws settings
AWS_ATTESTATION_BUCKET="${STACK_NAME}-attestation"
AWS_KEYS_BUCKET="${STACK_NAME}-keys"
AWS_ENCRYPTED_KEYS_BUCKET="${STACK_NAME}-keys-encrypted"
AWS_RELEASE_BUCKET="${STACK_NAME}-release"
AWS_LOGS_BUCKET="${STACK_NAME}-logs"
AWS_SNS_ARN=$(aws --region ${REGION} sns list-topics --query 'Topics[0].TopicArn' --output text | cut -d":" -f1,2,3,4,5)":${STACK_NAME}"
INSTANCE_TYPE=$(curl -s http://169.254.169.254/latest/meta-data/instance-type)
INSTANCE_REGION=$(curl -s http://169.254.169.254/latest/dynamic/instance-identity/document | awk -F\" '/region/ {print $4}')
INSTANCE_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)

# build settings
SECONDS=0
BUILD_TARGET="release aosp_${DEVICE} ${BUILD_TYPE}"
RELEASE_URL="https://${AWS_RELEASE_BUCKET}.s3.amazonaws.com"
RELEASE_CHANNEL="${DEVICE}-${BUILD_CHANNEL}"
BUILD_DATE=$(date +%Y.%m.%d.%H)
BUILD_TIMESTAMP=$(date +%s)
BUILD_DIR="$HOME/graphenesnake-os"
KEYS_DIR="${BUILD_DIR}/keys"
CERTIFICATE_SUBJECT='/CN=GrapheneSnakeOS'
OFFICIAL_FDROID_KEY="43238d512c1e5eb2d6569f4a3afbf5523418b82e0a3ed1552770abb9a9c9ccab"
KERNEL_SOURCE_DIR="${HOME}/kernel/google/${DEVICE_FAMILY}"
BUILD_REASON=""

# urls
ANDROID_SDK_URL="https://dl.google.com/android/repository/sdk-tools-linux-4333796.zip"
MANIFEST_URL="https://github.com/GrapheneOS/platform_manifest"
FDROID_CLIENT_URL_LATEST="https://gitlab.com/api/v4/projects/36189/repository/tags"
FDROID_PRIV_EXT_URL_LATEST="https://gitlab.com/api/v4/projects/1481578/repository/tags"
KERNEL_SOURCE_URL="https://github.com/GrapheneOS/kernel_google_${DEVICE_FAMILY}"
AOSP_URL_BUILD="https://developers.google.com/android/images"
AOSP_URL_BRANCH="https://source.android.com/setup/start/build-numbers"

STACK_UPDATE_MESSAGE=
LATEST_STACK_VERSION=
FDROID_CLIENT_VERSION=
FDROID_PRIV_EXT_VERSION=
get_latest_versions() {
  log_header ${FUNCNAME}

  sudo DEBIAN_FRONTEND=noninteractive apt-get -y install jq

  # fdroid - get latest non alpha tags from gitlab (sorted)
  # TODO: exclude alpha once 1.8 stable is released
  FDROID_CLIENT_VERSION=$(curl --fail -s "$FDROID_CLIENT_URL_LATEST" | jq -r '[.[] | select(.name | test("^[0-9]+\\.[0-9]+")) | select(.name | contains("ota") | not)][] | .name' | sort --version-sort -r | head -1)
  if [ -z "$FDROID_CLIENT_VERSION" ]; then
    aws_notify_simple "ERROR: Unable to get latest F-Droid version details. Stopping build."
    exit 1
  fi
  FDROID_PRIV_EXT_VERSION=$(curl --fail -s "$FDROID_PRIV_EXT_URL_LATEST" | jq -r '[.[] | select(.name | test("^[0-9]+\\.[0-9]+")) | select(.name | contains("alpha") | not) | select(.name | contains("ota") | not)][] | .name' | sort --version-sort -r | head -1)
  if [ -z "$FDROID_PRIV_EXT_VERSION" ]; then
    aws_notify_simple "ERROR: Unable to get latest F-Droid privilege extension version details. Stopping build."
    exit 1
  fi

  # attempt to automatically pick latest build version and branch. note this is likely to break with any page redesign. should also add some validation here.
  if [ -z "$AOSP_BUILD" ]; then
    AOSP_BUILD=$(curl --fail -s ${AOSP_URL_BUILD} | grep -A1 "${DEVICE}" | egrep '[a-zA-Z]+ [0-9]{4}\)' | grep -F "${ANDROID_VERSION}" | tail -1 | cut -d"(" -f2 | cut -d"," -f1)
    if [ -z "$AOSP_BUILD" ]; then
      aws_notify_simple "ERROR: Unable to get latest AOSP build information. Stopping build. This lookup is pretty fragile and can break on any page redesign of ${AOSP_URL_BUILD}"
      exit 1
    fi
  fi
  if [ -z "$AOSP_BRANCH" ]; then
    AOSP_BRANCH=$(curl --fail -s ${AOSP_URL_BRANCH} | grep -A1 "${AOSP_BUILD}" | tail -1 | cut -f2 -d">"|cut -f1 -d"<")
    if [ -z "$AOSP_BRANCH" ]; then
      aws_notify_simple "ERROR: Unable to get latest AOSP branch information. Stopping build. This can happen if ${AOSP_URL_BRANCH} hasn't been updated yet with newly released factory images."
      exit 1
    fi
  fi
}

check_for_new_versions() {
  log_header ${FUNCNAME}

  echo "Checking if any new versions of software exist"
  needs_update=false

  # check aosp
  existing_aosp_build=$(aws s3 cp "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-vendor" - || true)
  if [ "$existing_aosp_build" == "$AOSP_BUILD" ]; then
    echo "AOSP build ($existing_aosp_build) is up to date"
  else
    echo "AOSP needs to be updated to ${AOSP_BUILD}"
    needs_update=true
    BUILD_REASON="$BUILD_REASON 'AOSP build $existing_aosp_build != $AOSP_BUILD'"
  fi

  # check fdroid
  existing_fdroid_client=$(aws s3 cp "s3://${AWS_RELEASE_BUCKET}/fdroid/revision" - || true)
  if [ "$existing_fdroid_client" == "$FDROID_CLIENT_VERSION" ]; then
    echo "F-Droid build ($existing_fdroid_client) is up to date"
  else
    echo "F-Droid needs to be updated to ${FDROID_CLIENT_VERSION}"
    needs_update=true
    BUILD_REASON="$BUILD_REASON 'F-Droid version $existing_fdroid_client != $FDROID_CLIENT_VERSION'"
  fi

  # check fdroid priv extension
  existing_fdroid_priv_version=$(aws s3 cp "s3://${AWS_RELEASE_BUCKET}/fdroid-priv/revision" - || true)
  if [ "$existing_fdroid_priv_version" == "$FDROID_PRIV_EXT_VERSION" ]; then
    echo "F-Droid privileged extension build ($existing_fdroid_priv_version) is up to date"
  else
    echo "F-Droid privileged extension needs to be updated to ${FDROID_PRIV_EXT_VERSION}"
    needs_update=true
    BUILD_REASON="$BUILD_REASON 'F-Droid privileged extension $existing_fdroid_priv_version != $FDROID_PRIV_EXT_VERSION'"
  fi

  if [ "$needs_update" = true ]; then
    echo "New build is required"
  else
    if [ "$FORCE_BUILD" = true ]; then
      message="No build is required, but FORCE_BUILD=true"
      echo "$message"
      BUILD_REASON="$message"
    elif [ "$IGNORE_VERSION_CHECKS" = true ]; then
      message="No build is required, but IGNORE_VERSION_CHECKS=true"
      echo "$message"
      BUILD_REASON="$message"
    else
      aws_notify "GrapheneSnakeOS build not required as all components are already up to date."
      exit 0
    fi
  fi

  if [ -z "$existing_stack_version" ]; then
    BUILD_REASON="Initial build"
  fi
}

full_run() {
  log_header ${FUNCNAME}

  get_latest_versions
  check_for_new_versions
  initial_key_setup
  aws_notify "GrapheneSnakeOS Build STARTED"
  setup_env
  aosp_repo_init
  aosp_repo_modifications
  aosp_repo_sync
  aws_import_keys
  if [ "${ENABLE_ATTESTATION}" == "true" ]; then
    attestation_setup
  fi
  setup_vendor
  build_fdroid
  apply_patches
  # Rebuild kernel using GrapheneOS build script
  rebuild_kernel
  build_aosp
  release "${DEVICE}"
  aws_upload
  checkpoint_versions
  aws_notify "GrapheneSnakeOS Build SUCCESS"
}

build_fdroid() {
  log_header ${FUNCNAME}

  # build it outside AOSP build tree or hit errors
  git clone https://gitlab.com/fdroid/fdroidclient ${HOME}/fdroidclient
  pushd ${HOME}/fdroidclient
  echo "sdk.dir=${HOME}/sdk" > local.properties
  echo "sdk.dir=${HOME}/sdk" > app/local.properties
  git checkout $FDROID_CLIENT_VERSION
  retry ./gradlew assembleRelease
  cp -f app/build/outputs/apk/full/release/app-full-release-unsigned.apk ${BUILD_DIR}/packages/apps/F-Droid/F-Droid.apk
  popd
}

attestation_setup() {
  sudo DEBIAN_FRONTEND=noninteractive apt-get -y install libffi-dev

  cd $HOME
  echo "cloning beanstalk cli"
  git clone https://github.com/aws/aws-elastic-beanstalk-cli-setup.git
  retry ./aws-elastic-beanstalk-cli-setup/scripts/bundled_installer

  PLATFORM_CERT_SHA256=$(openssl x509 -noout -fingerprint -sha256 -inform pem -in ${KEYS_DIR}/${DEVICE}/platform.x509.pem | awk -F"=" '{print $2}' | sed 's/://g')
  ATTESTATION_DOMAIN=$(aws --region ${REGION} elasticbeanstalk describe-environments | jq -r '.Environments[] | select(.EnvironmentName=="attestation" and .Status!="Terminated") | .CNAME')
  echo "ATTESTATION_DOMAIN: ${ATTESTATION_DOMAIN}"
  echo "PLATFORM_CERT_SHA256: ${PLATFORM_CERT_SHA256}"

  OG_PIXEL3_FINGERPRINT="0F9A9CC8ADE73064A54A35C5509E77994E3AA37B6FB889DD53AF82C3C570C5CF"
  OG_PIXEL3_XL_FINGERPRINT="06DD526EE9B1CB92AA19D9835B68B4FF1A48A3AD31D813F27C9A7D6C271E9451"
  OG_PIXEL3A_FINGERPRINT="3ADD526EE9B1CB92AA19D9835B68B4FF1A48A3AD31D813F27C9A7D6C271E9451"

  PIXEL3_FINGERPRINT=${OG_PIXEL3_FINGERPRINT}
  PIXEL3_XL_FINGERPRINT=${OG_PIXEL3_XL_FINGERPRINT}
  PIXEL3A_FINGERPRINT=${OG_PIXEL3A_FINGERPRINT}
  if [ "${DEVICE}" == "blueline" ]; then
    PIXEL3_FINGERPRINT=$(cat ${KEYS_DIR}/${DEVICE}/avb_pkmd.bin | sha256sum | awk '{print $1}' | awk '{ print toupper($0) }')
  fi
  if [ "${DEVICE}" == "crosshatch" ]; then
    PIXEL3_XL_FINGERPRINT=$(cat ${KEYS_DIR}/${DEVICE}/avb_pkmd.bin | sha256sum | awk '{print $1}' | awk '{ print toupper($0) }')
  fi
  if [ "${DEVICE}" == "sargo" ] || [ "${DEVICE}" == "bonito" ]; then
    PIXEL3A_FINGERPRINT=$(cat ${KEYS_DIR}/${DEVICE}/avb_pkmd.bin | sha256sum | awk '{print $1}' | awk '{ print toupper($0) }')
  fi

  cd $HOME
  echo "cloning and building auditor"
  git clone https://github.com/GrapheneOS/Auditor.git
  cd Auditor
  sed -i "s/DOMAIN_NAME/${ATTESTATION_DOMAIN}/g" app/src/main/res/values/strings.xml
  sed -i "s/attestation.app/${ATTESTATION_DOMAIN}/" app/src/main/java/app/attestation/auditor/RemoteVerifyJob.java
  if [ "${DEVICE}" == "blueline" ]; then
    sed -i "s/${OG_PIXEL3_FINGERPRINT}/${PIXEL3_FINGERPRINT}/g" app/src/main/java/app/attestation/auditor/AttestationProtocol.java
  fi
  if [ "${DEVICE}" == "crosshatch" ]; then
    sed -i "s/${OG_PIXEL3_XL_FINGERPRINT}/${PIXEL3_XL_FINGERPRINT}/g" app/src/main/java/app/attestation/auditor/AttestationProtocol.java
  fi
  if [ "${DEVICE}" == "sargo" ] || [ "${DEVICE}" == "bonito" ]; then
    sed -i "s/${OG_PIXEL3A_FINGERPRINT}/${PIXEL3A_FINGERPRINT}/g" app/src/main/java/app/attestation/auditor/AttestationProtocol.java
  fi
  sed -i "s/990E04F0864B19F14F84E0E432F7A393F297AB105A22C1E1B10B442A4A62C42C/${PLATFORM_CERT_SHA256}/" app/src/main/java/app/attestation/auditor/AttestationProtocol.java
  echo "sdk.dir=${HOME}/sdk" > local.properties
  echo "sdk.dir=${HOME}/sdk" > app/local.properties
  ./gradlew build && ./gradlew assembleRelease
  mkdir -p ${BUILD_DIR}/external/Auditor/prebuilt
  cp app/build/outputs/apk/release/app-release-unsigned.apk ${BUILD_DIR}/external/Auditor/prebuilt/Auditor.apk

  cd $HOME
  echo "cloning attestationserver"
  git clone https://github.com/GrapheneOS/AttestationServer.git
  cd AttestationServer
  cat <<EOF > .ebextensions/.config
option_settings:
- option_name: DOMAIN_NAME
  value: ${ATTESTATION_DOMAIN}
- option_name: FINGERPRINT_PIXEL3
  value: ${PIXEL3_FINGERPRINT}
- option_name: FINGERPRINT_PIXEL3_XL
  value: ${PIXEL3_XL_FINGERPRINT}
- option_name: FINGERPRINT_PIXEL3A
  value: ${PIXEL3A_FINGERPRINT}
- option_name: SNS_ARN
  value: ${AWS_SNS_ARN}
- option_name: REGION
  value: ${REGION}
- option_name: EC2_SPOT_PRICE
  value: ${ATTESTATION_MAX_SPOT_PRICE}
- option_name: S3_BACKUP_BUCKET
  value: s3://${AWS_ATTESTATION_BUCKET}
- option_name: ATTESTATION_APP_SIGNATURE_DIGEST_RELEASE
  value: ${PLATFORM_CERT_SHA256}
EOF
  sed -i "s/STACK_NAME/${STACK_NAME}/g" .ebextensions/01-setup.config
  sed -i "s/STACK_NAME/${STACK_NAME}/g" .ebextensions/sqlite-backup-restore.sh

  mkdir -p .elasticbeanstalk
  cat <<EOF > .elasticbeanstalk/config.yml
branch-defaults:
  master:
    environment: attestation
    group_suffix: null
environment-defaults:
  attestation:
    branch: null
    repository: null
global:
  application_name: ${AWS_ATTESTATION_BUCKET}
  branch: null
  default_ec2_keyname: null
  default_platform: docker
  default_region: ${REGION}
  include_git_submodules: true
  instance_profile: null
  platform_name: null
  platform_version: null
  profile: null
  repository: null
  sc: git
  workspace_type: Application
EOF

  echo "deploying eb environment"
  $HOME/.ebcli-virtual-env/executables/eb deploy attestation -nh
}

get_encryption_key() {
  additional_message=""
  if [ "$(aws s3 ls "s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE}" | wc -l)" == '0' ]; then
    additional_message="Since you have no encrypted signing keys in s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE} yet - new signing keys will be generated and encrypted with provided passphrase."
  fi

  wait_time="10m"
  error_message=""
  while [ 1 ]; do
    aws sns publish --region ${REGION} --topic-arn "$AWS_SNS_ARN" \
      --message="$(printf "%s Need to login to the EC2 instance and provide the encryption passphrase (${wait_time} timeout before shutdown). You may need to open up SSH in the default security group, see the FAQ for details. %s\n\nssh ubuntu@%s 'printf \"Enter encryption passphrase: \" && read -s k && echo \"\$k\" > %s'" "$error_message" "$additional_message" "${INSTANCE_IP}" "${ENCRYPTION_PIPE}")"
    error_message=""

    log "Waiting for encryption passphrase (with $wait_time timeout) to be provided over named pipe $ENCRYPTION_PIPE"
    set +e
    ENCRYPTION_KEY=$(timeout $wait_time cat $ENCRYPTION_PIPE)
    if [ $? -ne 0 ]; then
      set -e
      log "Timeout ($wait_time) waiting for encryption passphrase"
      aws_notify_simple "Timeout ($wait_time) waiting for encryption passphrase. Terminating build process."
      exit 1
    fi
    set -e
    if [ -z "$ENCRYPTION_KEY" ]; then
      error_message="ERROR: Empty encryption passphrase received - try again."
      log "$error_message"
      continue
    fi
    log "Received encryption passphrase over named pipe $ENCRYPTION_PIPE"

    if [ "$(aws s3 ls "s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE}" | wc -l)" == '0' ]; then
      log "No existing encrypting keys - new keys will be generated later in build process."
    else
      log "Verifying encryption passphrase is valid by syncing encrypted signing keys from S3 and decrypting"
      aws s3 sync "s3://${AWS_ENCRYPTED_KEYS_BUCKET}" "${KEYS_DIR}"

      decryption_error=false
      set +e
      for f in $(find "${KEYS_DIR}" -type f -name '*.gpg'); do
        output_file=$(echo $f | awk -F".gpg" '{print $1}')
        log "Decrypting $f to ${output_file}..."
        gpg -d --batch --passphrase "${ENCRYPTION_KEY}" $f > $output_file
        if [ $? -ne 0 ]; then
          log "Failed to decrypt $f"
          decryption_error=true
        fi
      done
      set -e
      if [ "$decryption_error" = true ]; then
        log
        error_message="ERROR: Failed to decrypt signing keys with provided passphrase - try again."
        log "$error_message"
        continue
      fi
    fi
    break
  done
}

initial_key_setup() {
  # setup in memory file system to hold keys
  log "Mounting in memory filesystem at ${KEYS_DIR} to hold keys"
  mkdir -p $KEYS_DIR
  sudo mount -t tmpfs -o size=20m tmpfs $KEYS_DIR || true

  # additional steps for getting encryption key up front
  if [ "$ENCRYPTED_KEYS" = true ]; then
    log "Encrypted keys option was specified"

    # send warning if user has selected encrypted keys option but still has normal keys
    if [ "$(aws s3 ls "s3://${AWS_KEYS_BUCKET}/${DEVICE}" | wc -l)" != '0' ]; then
      if [ "$(aws s3 ls "s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE}" | wc -l)" == '0' ]; then
        aws_notify_simple "It looks like you have selected --encrypted-keys option and have existing signing keys in s3://${AWS_KEYS_BUCKET}/${DEVICE} but you haven't migrated your keys to s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE}. This means new encrypted signing keys will be generated and you'll need to flash a new factory image on your device. If you want to keep your existing keys - cancel this build and follow the steps on migrating your keys in the FAQ."
      fi
    fi

    sudo DEBIAN_FRONTEND=noninteractive apt-get -y install gpg
    if [ ! -e "$ENCRYPTION_PIPE" ]; then
      mkfifo $ENCRYPTION_PIPE
    fi

    get_encryption_key
  fi
}

setup_env() {
  log_header ${FUNCNAME}

  # setup build dir
  mkdir -p "$BUILD_DIR"

  # install required packages
  sudo apt-get update
  sudo DEBIAN_FRONTEND=noninteractive apt-get -y install repo gperf jq openjdk-8-jdk git-core gnupg flex bison build-essential zip curl zlib1g-dev gcc-multilib g++-multilib libc6-dev-i386 lib32ncurses5-dev x11proto-core-dev libx11-dev lib32z-dev ccache libgl1-mesa-dev libxml2-utils xsltproc unzip python-networkx liblz4-tool pxz
  sudo DEBIAN_FRONTEND=noninteractive apt-get -y build-dep "linux-image-$(uname --kernel-release)"

  # temporary workaround as java 11 is default version and not compatible with sdkmanager
  sudo update-java-alternatives --jre-headless --jre --set java-1.8.0-openjdk-amd64 || true
  sudo update-java-alternatives --set java-1.8.0-openjdk-amd64 || true

  # setup android sdk (required for fdroid build)
  if [ ! -f "${HOME}/sdk/tools/bin/sdkmanager" ]; then
    mkdir -p ${HOME}/sdk
    cd ${HOME}/sdk
    retry wget ${ANDROID_SDK_URL} -O sdk-tools.zip
    unzip sdk-tools.zip
    yes | ./tools/bin/sdkmanager --licenses
    ./tools/android update sdk -u --use-sdk-wrapper
    # workaround for license issue with f-droid using older sdk (didn't spend time to debug issue further)
    yes | ./tools/bin/sdkmanager "build-tools;27.0.3" "platforms;android-27"
  fi

  # setup git
  git config --get --global user.name || git config --global user.name 'unknown'
  git config --get --global user.email || git config --global user.email 'unknown@localhost'
  git config --global color.ui true
}

aosp_repo_init() {
  log_header ${FUNCNAME}
  cd "${BUILD_DIR}"

  LATEST_TAG=$(git ls-remote --tags $MANIFEST_URL | grep -o 'refs/tags/QQ1A.[0-9]*\.[0-9]*\.[0-9]*' | sort -r | head -n 1 | grep -o '[^\/]*$')
  repo init -u "$MANIFEST_URL" -b refs/tags/$LATEST_TAG || true
  gpg --recv-keys 65EEFE022108E2B708CBFCF7F9E712E59AF5F22A
  gpg --recv-keys 4340D13570EF945E83810964E8AD3F819AB10E78
  cd ${BUILD_DIR}/.repo/manifests
  git verify-tag --raw $(git describe)
  cd ../..
}

aosp_repo_modifications() {
  log_header ${FUNCNAME}
  cd "${BUILD_DIR}"

  # make modifications to default AOSP
  if ! grep -q "GrapheneOS" .repo/manifest.xml; then
    # really ugly awk script to add additional repos to manifest
    awk -i inplace \
      -v ANDROID_VERSION="$ANDROID_VERSION" \
      -v FDROID_CLIENT_VERSION="$FDROID_CLIENT_VERSION" \
      -v FDROID_PRIV_EXT_VERSION="$FDROID_PRIV_EXT_VERSION" \
      '1;/<repo-hooks in-project=/{
      print "  ";
      print "  <remote name=\"github\" fetch=\"https://github.com/GrapheneOS/\" revision=\"" ANDROID_VERSION "\" />";
      print "  <remote name=\"fdroid\" fetch=\"https://gitlab.com/fdroid/\" />";
      <% if .CustomManifestRemotes %>
      <% range $i, $r := .CustomManifestRemotes %>
      print "  <remote name=\"<% .Name %>\" fetch=\"<% .Fetch %>\" revision=\"<% .Revision %>\" />";
      <% end %>
      <% end %>
      print "  ";
      <% if .CustomManifestProjects %><% range $i, $r := .CustomManifestProjects %>
      print "  <project path=\"<% .Path %>\" name=\"<% .Name %>\" remote=\"<% .Remote %>\" />";
      <% end %>
      <% end %>
      <% if .EnableAttestation %>
      print "  <project path=\"external/Auditor\" name=\"platform_external_Auditor\" remote=\"github\" />";
      <% end %>
      print "  <project path=\"packages/apps/Updater\" name=\"platform_packages_apps_Updater\" remote=\"github\" />";
      print "  <project path=\"packages/apps/F-Droid\" name=\"platform_external_fdroid\" remote=\"github\" />";
      print "  <project path=\"packages/apps/F-DroidPrivilegedExtension\" name=\"privileged-extension\" remote=\"fdroid\" revision=\"refs/tags/" FDROID_PRIV_EXT_VERSION "\" />";
      print "  <project path=\"vendor/android-prepare-vendor\" name=\"android-prepare-vendor\" remote=\"github\" />"}' .repo/manifest.xml

    # remove things from manifest
    sed -i '/packages\/apps\/Browser2/d' .repo/manifest.xml
    sed -i '/packages\/apps\/Calendar/d' .repo/manifest.xml
    sed -i '/packages\/apps\/QuickSearchBox/d' .repo/manifest.xml
  else
    log "Skipping modification of .repo/manifest.xml as they have already been made"
  fi
}

aosp_repo_sync() {
  log_header ${FUNCNAME}
  cd "${BUILD_DIR}"

  # sync with retries
  for i in {1..10}; do
    repo sync -c --no-tags --no-clone-bundle --jobs 32 && break
  done
  repo forall -c 'git verify-tag --raw $(git describe)' || echo Verification failed!
}

setup_vendor() {
  log_header ${FUNCNAME}

  # new dependency to extract ota partitions
  sudo DEBIAN_FRONTEND=noninteractive apt-get -y install python-protobuf

  # get vendor files (with timeout)
  timeout 30m "${BUILD_DIR}/vendor/android-prepare-vendor/execute-all.sh" --debugfs --keep --yes --device "${DEVICE}" --buildID "${AOSP_BUILD}" --output "${BUILD_DIR}/vendor/android-prepare-vendor"
  
  # copy vendor files to build tree
  mkdir --parents "${BUILD_DIR}/vendor/google_devices" || true
  rm -rf "${BUILD_DIR}/vendor/google_devices/$DEVICE" || true
  mv "${BUILD_DIR}/vendor/android-prepare-vendor/${DEVICE}/$(tr '[:upper:]' '[:lower:]' <<< "${AOSP_BUILD}")/vendor/google_devices/${DEVICE}" "${BUILD_DIR}/vendor/google_devices"

  # smaller devices need big brother vendor files
  if [ "$DEVICE" != "$DEVICE_FAMILY" ]; then
    rm -rf "${BUILD_DIR}/vendor/google_devices/$DEVICE_FAMILY" || true
    mv "${BUILD_DIR}/vendor/android-prepare-vendor/$DEVICE/$(tr '[:upper:]' '[:lower:]' <<< "${AOSP_BUILD}")/vendor/google_devices/$DEVICE_FAMILY" "${BUILD_DIR}/vendor/google_devices"
  fi
}

apply_patches() {
  log_header ${FUNCNAME}

  patch_custom
  patch_aosp_removals
  patch_add_apps
  patch_base_config
  patch_settings_app
  patch_device_config
  patch_updater
  patch_priv_ext
  patch_launcher
  patch_broken_alarmclock
  patch_broken_messaging
  patch_disable_apex
}

# currently don't have a need for apex updates (https://source.android.com/devices/tech/ota/apex)
patch_disable_apex() {
  log_header ${FUNCNAME}

  # pixel 1 devices do not support apex so nothing to patch
  # pixel 2 devices opt in here
  sed -i 's@$(call inherit-product, $(SRC_TARGET_DIR)/product/updatable_apex.mk)@@' ${BUILD_DIR}/device/google/wahoo/device.mk
  # all other devices use mainline and opt in here
  sed -i 's@$(call inherit-product, $(SRC_TARGET_DIR)/product/updatable_apex.mk)@@' ${BUILD_DIR}/build/make/target/product/mainline_system.mk
}

# TODO: remove once this once fix from upstream makes it into release branch
# https://android.googlesource.com/platform/packages/apps/DeskClock/+/e6351b3b85b2f5d53d43e4797d3346ce22a5fa6f%5E%21/
patch_broken_alarmclock() {
  log_header ${FUNCNAME}

  if ! grep -q "android.permission.FOREGROUND_SERVICE" ${BUILD_DIR}/packages/apps/DeskClock/AndroidManifest.xml; then
    sed -i '/<uses-permission android:name="android.permission.READ_EXTERNAL_STORAGE" \/>/a <uses-permission android:name="android.permission.FOREGROUND_SERVICE" \/>' ${BUILD_DIR}/packages/apps/DeskClock/AndroidManifest.xml
    sed -i 's@<uses-sdk android:minSdkVersion="19" android:targetSdkVersion="28" />@<uses-sdk android:minSdkVersion="19" android:targetSdkVersion="25" />@' ${BUILD_DIR}/packages/apps/DeskClock/AndroidManifest.xml
  fi
}

# TODO: remove once this once fix from upstream makes it into release branch
# https://android.googlesource.com/platform/packages/apps/Messaging/+/8e71d1b707123e1b48b5529b1661d53762922400%5E%21/
patch_broken_messaging() {
  log_header ${FUNCNAME}

  if ! grep -q "android:targetSdkVersion=\"24\"" ${BUILD_DIR}/packages/apps/Messaging/AndroidManifest.xml; then
    sed -i 's@<uses-sdk android:minSdkVersion="19" android:targetSdkVersion="28" />@<uses-sdk android:minSdkVersion="19" android:targetSdkVersion="24" />@' ${BUILD_DIR}/packages/apps/Messaging/AndroidManifest.xml
  fi
}

patch_aosp_removals() {
  log_header ${FUNCNAME}

  # loop over all make files as these keep changing and remove components
  for mk_file in ${BUILD_DIR}/build/make/target/product/*.mk; do
    # remove Browser2
    sed -i '/Browser2/d' ${mk_file}

    # remove Calendar
    sed -i '/Calendar \\/d' ${mk_file}
    sed -i '/Calendar.apk/d' ${mk_file}

    # remove QuickSearchBox
    sed -i '/QuickSearchBox/d' ${mk_file}
  done

}

# TODO: most of this is fragile and unforgiving
patch_custom() {
  log_header ${FUNCNAME}

  cd $BUILD_DIR

  # allow custom patches to be applied
  patches_dir="$HOME/patches"
  <% if .CustomPatches %>
  <% range $i, $r := .CustomPatches %>
    retry git clone <% $r.Repo %> ${patches_dir}/<% $i %>
    <% range $r.Patches %>
      log "Applying patch <% . %>"
      patch -p1 --no-backup-if-mismatch < ${patches_dir}/<% $i %>/<% . %>
    <% end %>
  <% end %>
  <% end %>

  # allow custom scripts to be applied
  scripts_dir="$HOME/scripts"
  <% if .CustomScripts %>
  <% range $i, $r := .CustomScripts %>
    retry git clone <% $r.Repo %> ${scripts_dir}/<% $i %>
    <% range $r.Scripts %>
      log "Applying shell script <% . %>"
      . ${scripts_dir}/<% $i %>/<% . %>
    <% end %>
  <% end %>
  <% end %>

  # allow prebuilt applications to be added to build tree
  prebuilt_dir="$BUILD_DIR/packages/apps/Custom"
  <% if .CustomPrebuilts %>
  <% range $i, $r := .CustomPrebuilts %>
    log "Putting custom prebuilts from <% $r.Repo %> in build tree location ${prebuilt_dir}/<% $i %>"
    retry git clone <% $r.Repo %> ${prebuilt_dir}/<% $i %>
    <% range .Modules %>
      log "Adding custom PRODUCT_PACKAGES += <% . %> to $(get_package_mk_file)"
      sed -i "\$aPRODUCT_PACKAGES += <% . %>" $(get_package_mk_file)
    <% end %>
  <% end %>
  <% end %>

  # allow custom hosts file
  hosts_file_location="$BUILD_DIR/system/core/rootdir/etc/hosts"
  if [ -z "$HOSTS_FILE" ]; then
    log "No custom hosts file requested"
  else
    log "Replacing hosts file with $HOSTS_FILE"
    retry wget -O $hosts_file_location "$HOSTS_FILE"
  fi
}

patch_base_config() {
  log_header ${FUNCNAME}

  # enable swipe up gesture functionality as option
  sed -i 's@<bool name="config_swipe_up_gesture_setting_available">false</bool>@<bool name="config_swipe_up_gesture_setting_available">true</bool>@' ${BUILD_DIR}/frameworks/base/core/res/res/values/config.xml

  # enable doze and app standby
  sed -i 's@<bool name="config_enableAutoPowerModes">false</bool>@<bool name="config_enableAutoPowerModes">true</bool>@' ${BUILD_DIR}/frameworks/base/core/res/res/values/config.xml
}

patch_settings_app() {
  log_header ${FUNCNAME}

  # fix for cards not disappearing in settings app
  sed -i 's@<bool name="config_use_legacy_suggestion">true</bool>@<bool name="config_use_legacy_suggestion">false</bool>@' ${BUILD_DIR}/packages/apps/Settings/res/values/config.xml
}

patch_device_config() {
  log_header ${FUNCNAME}

  # set proper model names
  sed -i 's@PRODUCT_MODEL := AOSP on msm8996@PRODUCT_MODEL := Pixel XL@' ${BUILD_DIR}/device/google/marlin/aosp_marlin.mk
  sed -i 's@PRODUCT_MANUFACTURER := google@PRODUCT_MANUFACTURER := Google@' ${BUILD_DIR}/device/google/marlin/aosp_marlin.mk
  sed -i 's@PRODUCT_MODEL := AOSP on msm8996@PRODUCT_MODEL := Pixel@' ${BUILD_DIR}/device/google/marlin/aosp_sailfish.mk
  sed -i 's@PRODUCT_MANUFACTURER := google@PRODUCT_MANUFACTURER := Google@' ${BUILD_DIR}/device/google/marlin/aosp_sailfish.mk

  sed -i 's@PRODUCT_MODEL := AOSP on taimen@PRODUCT_MODEL := Pixel 2 XL@' ${BUILD_DIR}/device/google/taimen/aosp_taimen.mk
  sed -i 's@PRODUCT_MODEL := AOSP on walleye@PRODUCT_MODEL := Pixel 2@' ${BUILD_DIR}/device/google/muskie/aosp_walleye.mk

  sed -i 's@PRODUCT_MODEL := AOSP on crosshatch@PRODUCT_MODEL := Pixel 3 XL@' ${BUILD_DIR}/device/google/crosshatch/aosp_crosshatch.mk || true
  sed -i 's@PRODUCT_MODEL := AOSP on blueline@PRODUCT_MODEL := Pixel 3@' ${BUILD_DIR}/device/google/crosshatch/aosp_blueline.mk || true

  sed -i 's@PRODUCT_MODEL := AOSP on bonito@PRODUCT_MODEL := Pixel 3a XL@' ${BUILD_DIR}/device/google/bonito/aosp_bonito.mk || true
  sed -i 's@PRODUCT_MODEL := AOSP on sargo@PRODUCT_MODEL := Pixel 3a@' ${BUILD_DIR}/device/google/bonito/aosp_sargo.mk || true
}

get_package_mk_file() {
  mk_file=${BUILD_DIR}/build/make/target/product/handheld_system.mk
  if [ ! -f ${mk_file} ]; then
    log "Expected handheld_system.mk or core.mk do not exist"
    exit 1
  fi
  echo ${mk_file}
}

patch_add_apps() {
  log_header ${FUNCNAME}

  mk_file=$(get_package_mk_file)
  sed -i "\$aPRODUCT_PACKAGES += Updater" ${mk_file}
  sed -i "\$aPRODUCT_PACKAGES += F-DroidPrivilegedExtension" ${mk_file}
  sed -i "\$aPRODUCT_PACKAGES += F-Droid" ${mk_file}
  if [ "${ENABLE_ATTESTATION}" == "true" ]; then
    sed -i "\$aPRODUCT_PACKAGES += Auditor" ${mk_file}
  fi

  # add any modules defined in custom manifest projects
  <% if .CustomManifestProjects %><% range $i, $r := .CustomManifestProjects %><% range $j, $q := .Modules %>
  log "Adding custom PRODUCT_PACKAGES += <% $q %> to ${mk_file}"
  sed -i "\$aPRODUCT_PACKAGES += <% $q %>" ${mk_file}
  <% end %>
  <% end %>
  <% end %>
}

patch_updater() {
  log_header ${FUNCNAME}

  cd "$BUILD_DIR"/packages/apps/Updater/res/values
  sed --in-place --expression "s@s3bucket@${RELEASE_URL}/@g" config.xml
}

fdpe_hash() {
  keytool -list -printcert -file "$1" | grep 'SHA256:' | tr --delete ':' | cut --delimiter ' ' --fields 3
}

patch_priv_ext() {
  log_header ${FUNCNAME}

  # 0.2.9 added whitelabel support, so BuildConfig.APPLICATION_ID needs to be set now
  sed -i 's@BuildConfig.APPLICATION_ID@"org.fdroid.fdroid.privileged"@' ${BUILD_DIR}/packages/apps/F-DroidPrivilegedExtension/app/src/main/java/org/fdroid/fdroid/privileged/PrivilegedService.java

  unofficial_releasekey_hash=$(fdpe_hash "${KEYS_DIR}/${DEVICE}/releasekey.x509.pem")
  unofficial_platform_hash=$(fdpe_hash "${KEYS_DIR}/${DEVICE}/platform.x509.pem")
  sed -i 's/'${OFFICIAL_FDROID_KEY}'")/'${unofficial_releasekey_hash}'"),\n            new Pair<>("org.fdroid.fdroid", "'${unofficial_platform_hash}'")/' \
      "${BUILD_DIR}/packages/apps/F-DroidPrivilegedExtension/app/src/main/java/org/fdroid/fdroid/privileged/ClientWhitelist.java"
}

patch_launcher() {
  log_header ${FUNCNAME}

  # disable QuickSearchBox widget on home screen
  sed -i.original "s/QSB_ON_FIRST_SCREEN = true;/QSB_ON_FIRST_SCREEN = false;/" "${BUILD_DIR}/packages/apps/Launcher3/src/com/android/launcher3/config/BaseFlags.java"
  # fix compile error with uninitialized variable
  sed -i.original "s/boolean createEmptyRowOnFirstScreen;/boolean createEmptyRowOnFirstScreen = false;/" "${BUILD_DIR}/packages/apps/Launcher3/src/com/android/launcher3/provider/ImportDataTask.java"
}

rebuild_kernel() {
  log_header ${FUNCNAME}

  # checkout kernel source on proper commit
  mkdir -p "${KERNEL_SOURCE_DIR}"
  cd "${KERNEL_SOURCE_DIR}"
  git submodule sync
  git submodule update --init
  # TODO: make this a bit more robust
  kernel_commit_id=$(lz4cat "${BUILD_DIR}/device/google/${DEVICE_FAMILY}-kernel/Image.lz4-dtb" | grep -a 'Linux version' | cut -d ' ' -f3 | cut -d'-' -f2 | sed 's/^g//g')
 
  log "Checking out kernel commit ${kernel_commit_id}"
  git checkout ${kernel_commit_id}
  # run in another shell to avoid it mucking with environment variables for normal AOSP build
  (
      set -e;
      cd ${KERNEL_SOURCE_DIR};
      ln --verbose --symbolic ${KEYS_DIR}/${DEVICE}/verity_user.der.x509 ${KERNEL_SOURCE_DIR}/verity_user.der.x509;
      ./build.sh ${DEVICE}
      rm -rf ${BUILD_DIR}/out/build_*;
  )
}

build_aosp() {
  log_header ${FUNCNAME}

  cd "$BUILD_DIR"

  ############################
  # from original setup.sh script
  ############################
  source script/envsetup.sh
  log "BUILD_NUMBER=$BUILD_NUMBER"

  choosecombo $BUILD_TARGET
  log "Running target-files-package"
  retry make -j $(nproc) target-files-package
  log "Running brillo_update_payload"
  retry make -j $(nproc) brillo_update_payload
}

get_radio_image() {
  grep -Po "require version-$1=\K.+" vendor/$2/vendor-board-info.txt | tr '[:upper:]' '[:lower:]'
}

release() {
  log_header ${FUNCNAME}

  cd "$BUILD_DIR"

  ############################
  # from original setup.sh script
  ############################
  source script/envsetup.sh
  log "BUILD_NUMBER=$BUILD_NUMBER"

  ############################
  # from original release.sh script
  ############################
  KEY_DIR=keys/$1
  OUT=out/release-$1-${BUILD_NUMBER}
  source device/common/clear-factory-images-variables.sh

  DEVICE=$1
  BOOTLOADER=$(get_radio_image bootloader google_devices/${DEVICE})
  RADIO=$(get_radio_image baseband google_devices/${DEVICE})
  PREFIX=aosp_
  BUILD=$BUILD_NUMBER
  VERSION=$(grep -Po "BUILD_ID=\K.+" build/core/build_id.mk | tr '[:upper:]' '[:lower:]')
  PRODUCT=${DEVICE}
  TARGET_FILES=$DEVICE-target_files-$BUILD.zip

  # make sure output directory exists
  mkdir -p $OUT

  # depending on device need verity key or avb key
  case "${AVB_MODE}" in
    verity_only)
      AVB_SWITCHES=(--replace_verity_public_key "$KEY_DIR/verity_key.pub"
                    --replace_verity_private_key "$KEY_DIR/verity"
                    --replace_verity_keyid "$KEY_DIR/verity.x509.pem")
      ;;
    vbmeta_simple)
      # Pixel 2: one vbmeta struct, no chaining
      AVB_SWITCHES=(--avb_vbmeta_key "$KEY_DIR/avb.pem"
                    --avb_vbmeta_algorithm SHA256_RSA2048)
      ;;
    vbmeta_chained)
      # Pixel 3: main vbmeta struct points to a chained vbmeta struct in system.img
      AVB_SWITCHES=(--avb_vbmeta_key "$KEY_DIR/avb.pem"
                    --avb_vbmeta_algorithm SHA256_RSA2048
                    --avb_system_key "$KEY_DIR/avb.pem"
                    --avb_system_algorithm SHA256_RSA2048)
      ;;
  esac

  export PATH=$BUILD_DIR/prebuilts/build-tools/linux-x86/bin:$PATH

  log "Running sign_target_files_apks"
  build/tools/releasetools/sign_target_files_apks -o -d "$KEY_DIR" -k "build/target/product/security/networkstack=${KEY_DIR}/networkstack" "${AVB_SWITCHES[@]}" \
    out/target/product/$DEVICE/obj/PACKAGING/target_files_intermediates/$PREFIX$DEVICE-target_files-$BUILD_NUMBER.zip \
    $OUT/$TARGET_FILES

  log "Running ota_from_target_files"
  build/tools/releasetools/ota_from_target_files --block -k "$KEY_DIR/releasekey" "${EXTRA_OTA[@]}" $OUT/$TARGET_FILES \
      $OUT/$DEVICE-ota_update-$BUILD.zip

  log "Running img_from_target_files"
  sed -i 's/zipfile\.ZIP_DEFLATED/zipfile\.ZIP_STORED/' build/tools/releasetools/img_from_target_files.py
  build/tools/releasetools/img_from_target_files $OUT/$TARGET_FILES $OUT/$DEVICE-img-$BUILD.zip

  log "Running generate-factory-images"
  cd $OUT
  sed -i 's/zip -r/tar cvf/' ../../device/common/generate-factory-images-common.sh
  sed -i 's/factory\.zip/factory\.tar/' ../../device/common/generate-factory-images-common.sh
  sed -i '/^mv / d' ../../device/common/generate-factory-images-common.sh
  source ../../device/common/generate-factory-images-common.sh
  mv $DEVICE-$VERSION-factory.tar $DEVICE-factory-$BUILD_NUMBER.tar
  rm -f $DEVICE-factory-$BUILD_NUMBER.tar.xz

  log "Running compress of factory image with pxz"
  time pxz -v -T0 -9 -z $DEVICE-factory-$BUILD_NUMBER.tar
}

# TODO: cleanup this function
aws_upload() {
  log_header ${FUNCNAME}

  cd "${BUILD_DIR}/out"
  build_date="$(< build_number.txt)"
  build_timestamp="$(unzip -p "release-${DEVICE}-${build_date}/${DEVICE}-ota_update-${build_date}.zip" META-INF/com/android/metadata | grep 'post-timestamp' | cut --delimiter "=" --fields 2)"

  # copy ota file to s3, update file metadata used by updater app, and remove old ota files
  read -r old_metadata <<< "$(wget -O - "${RELEASE_URL}/${RELEASE_CHANNEL}")"
  old_date="$(cut -d ' ' -f 1 <<< "${old_metadata}")"
  (
    aws s3 cp "${BUILD_DIR}/out/release-${DEVICE}-${build_date}/${DEVICE}-ota_update-${build_date}.zip" "s3://${AWS_RELEASE_BUCKET}" --acl public-read &&
    echo "${build_date} ${build_timestamp} ${AOSP_BUILD}" | aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/${RELEASE_CHANNEL}" --acl public-read &&
    echo "${BUILD_TIMESTAMP}" | aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/${RELEASE_CHANNEL}-true-timestamp" --acl public-read
  ) && ( aws s3 rm "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-ota_update-${old_date}.zip" || true )

  # upload factory image
  retry aws s3 cp "${BUILD_DIR}/out/release-${DEVICE}-${build_date}/${DEVICE}-factory-${build_date}.tar.xz" "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-factory-latest.tar.xz"

  # cleanup old target files if some exist
  if [ "$(aws s3 ls "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-target" | wc -l)" != '0' ]; then
    cleanup_target_files
  fi

  # copy new target file to s3
  retry aws s3 cp "${BUILD_DIR}/out/release-${DEVICE}-${build_date}/${DEVICE}-target_files-${build_date}.zip" "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-target/${DEVICE}-target-files-${build_date}.zip"
}

cleanup_target_files() {
  log_header ${FUNCNAME}

  aws s3 sync "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-target" "${BUILD_DIR}/${DEVICE}-target"
  cd "${BUILD_DIR}/${DEVICE}-target"
  for target_file in ${DEVICE}-target-files-*.zip ; do
    old_date=$(echo "$target_file" | cut --delimiter "-" --fields 4 | cut --delimiter "." --fields 5 --complement)
    aws s3 rm "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-target/${DEVICE}-target-files-${old_date}.zip" || true
  done
}

checkpoint_versions() {
  log_header ${FUNCNAME}

  # checkpoint stack version
  echo "${STACK_VERSION}" | aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/graphenesnakeos-stack/revision"

  # checkpoint f-droid
  echo "${FDROID_PRIV_EXT_VERSION}" | aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/fdroid-priv/revision"
  echo "${FDROID_CLIENT_VERSION}" | aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/fdroid/revision"
  
  # checkpoint aosp
  aws s3 cp - "s3://${AWS_RELEASE_BUCKET}/${DEVICE}-vendor" --acl public-read <<< "${AOSP_BUILD}" || true
  
}

aws_notify_simple() {
  log_header ${FUNCNAME}

  aws sns publish --region ${REGION} --topic-arn "$AWS_SNS_ARN" --message "$1"
}

aws_notify() {
  log_header ${FUNCNAME}

  LOGOUTPUT=
  if [ ! -z "$2" ]; then
    LOGOUTPUT=$(tail -c 20000 /var/log/cloud-init-output.log)
  fi
  ELAPSED="$(($SECONDS / 3600))hrs $((($SECONDS / 60) % 60))min $(($SECONDS % 60))sec"
  aws sns publish --region ${REGION} --topic-arn "$AWS_SNS_ARN" \
    --message="$(printf "$1\n  Device: %s\n  Stack Name: %s\n  Stack Version: %s %s\n  Stack Region: %s\n  Release Channel: %s\n  Instance Type: %s\n  Instance Region: %s\n  Instance IP: %s\n  Build Date: %s\n  Elapsed Time: %s\n  AOSP Build: %s\n  AOSP Branch: %s\n  F-Droid Version: %s\n  F-Droid Priv Extension Version: %s\n  Build Reason: %s\n%s" \
      "${DEVICE}" "${STACK_NAME}" "${STACK_VERSION}" "${STACK_UPDATE_MESSAGE}" "${REGION}" "${RELEASE_CHANNEL}" "${INSTANCE_TYPE}" "${INSTANCE_REGION}" "${INSTANCE_IP}" "${BUILD_DATE}" "${ELAPSED}" "${AOSP_BUILD}" "${AOSP_BRANCH}" "${FDROID_CLIENT_VERSION}" "${FDROID_PRIV_EXT_VERSION}" "${BUILD_REASON}" "${LOGOUTPUT}")" || true
}

aws_logging() {
  log_header ${FUNCNAME}

  df -h
  du -chs "${BUILD_DIR}" || true
  uptime
  aws s3 cp /var/log/cloud-init-output.log "s3://${AWS_LOGS_BUCKET}/${DEVICE}/$(date +%s)"
}

aws_import_keys() {
  log_header ${FUNCNAME}

  if [ "$ENCRYPTED_KEYS" = true ]; then
    if [ "$(aws s3 ls "s3://${AWS_ENCRYPTED_KEYS_BUCKET}/${DEVICE}" | wc -l)" == '0' ]; then
      log "No encrypted keys were found - generating encrypted keys"
      gen_keys
      for f in $(find "${KEYS_DIR}" -type f); do
        log "Encrypting file ${f} to ${f}.gpg"
        gpg --symmetric --batch --passphrase "$ENCRYPTION_KEY" --cipher-algo AES256 $f
      done
      log "Syncing encrypted keys to S3 s3://${AWS_ENCRYPTED_KEYS_BUCKET}"
      aws s3 sync "${KEYS_DIR}" "s3://${AWS_ENCRYPTED_KEYS_BUCKET}" --exclude "*" --include "*.gpg"
    fi
  else
    if [ "$(aws s3 ls "s3://${AWS_KEYS_BUCKET}/${DEVICE}" | wc -l)" == '0' ]; then
      log "No keys were found - generating keys"
      gen_keys
      log "Syncing keys to S3 s3://${AWS_KEYS_BUCKET}"
      aws s3 sync "${KEYS_DIR}" "s3://${AWS_KEYS_BUCKET}"
    else
      log "Keys already exist for ${DEVICE} - syncing them from S3"
      aws s3 sync "s3://${AWS_KEYS_BUCKET}" "${KEYS_DIR}"
    fi
  fi

  # handle migration with new networkstack key for 10.0
  pushd "${KEYS_DIR}/${DEVICE}"
  if [ ! -f "${KEYS_DIR}/${DEVICE}/networkstack.pk8" ]; then
    log "Did not find networkstack key - generating one"
    ! "${BUILD_DIR}/development/tools/make_key" "networkstack" "$CERTIFICATE_SUBJECT"

    if [ "$ENCRYPTED_KEYS" = true ]; then
      log "Encrypting and uploading new networkstack key to s3://${AWS_ENCRYPTED_KEYS_BUCKET}"
      gpg --symmetric --batch --passphrase "$ENCRYPTION_KEY" --cipher-algo AES256 networkstack.pk8
      gpg --symmetric --batch --passphrase "$ENCRYPTION_KEY" --cipher-algo AES256 networkstack.x509.pem
      aws s3 sync "${KEYS_DIR}" "s3://${AWS_ENCRYPTED_KEYS_BUCKET}" --exclude "*" --include "*.gpg"
    else
      log "Uploading new networkstack key to s3://${AWS_KEYS_BUCKET}"
      aws s3 sync "${KEYS_DIR}" "s3://${AWS_KEYS_BUCKET}"
    fi
  fi
  popd
}

gen_keys() {
  log_header ${FUNCNAME}

  mkdir -p "${KEYS_DIR}/${DEVICE}"
  cd "${KEYS_DIR}/${DEVICE}"
  for key in {releasekey,platform,shared,media,networkstack,verity} ; do
    # make_key exits with unsuccessful code 1 instead of 0, need ! to negate
    ! "${BUILD_DIR}/development/tools/make_key" "$key" "$CERTIFICATE_SUBJECT"
  done

  if [ "${AVB_MODE}" == "verity_only" ]; then
    gen_verity_key "${DEVICE}"
  else
    gen_avb_key "${DEVICE}"
  fi
}

gen_avb_key() {
  log_header ${FUNCNAME}

  cd "$BUILD_DIR"
  openssl genrsa -out "${KEYS_DIR}/$1/avb.pem" 2048
  ${BUILD_DIR}/external/avb/avbtool extract_public_key --key "${KEYS_DIR}/$1/avb.pem" --output "${KEYS_DIR}/$1/avb_pkmd.bin"
}

gen_verity_key() {
  log_header ${FUNCNAME}
  cd "$BUILD_DIR"

  make -j 20 generate_verity_key
  "${BUILD_DIR}/out/host/linux-x86/bin/generate_verity_key" -convert "${KEYS_DIR}/$1/verity.x509.pem" "${KEYS_DIR}/$1/verity_key"
  make clobber
  openssl x509 -outform der -in "${KEYS_DIR}/$1/verity.x509.pem" -out "${KEYS_DIR}/$1/verity_user.der.x509"
}

cleanup() {
  rv=$?
  aws_logging
  if [ $rv -ne 0 ]; then
    aws_notify "GrapheneSnakeOS Build FAILED" 1
  fi
  if [ "${PREVENT_SHUTDOWN}" = true ]; then
    log "Skipping shutdown"
  else
    sudo shutdown -h now
  fi
}

log_header() {
  echo "=================================="
  echo "$(date "+%Y-%m-%d %H:%M:%S"): Running $1"
  echo "=================================="
}

log() {
  echo "$(date "+%Y-%m-%d %H:%M:%S"): $1"
}

retry() {
  set +e
  local max_attempts=${ATTEMPTS-3}
  local timeout=${TIMEOUT-1}
  local attempt=0
  local exitCode=0

  while [[ $attempt < $max_attempts ]]
  do
    "$@"
    exitCode=$?

    if [[ $exitCode == 0 ]]
    then
      break
    fi

    log "Failure! Retrying ($@) in $timeout.."
    sleep $timeout
    attempt=$(( attempt + 1 ))
    timeout=$(( timeout * 2 ))
  done

  if [[ $exitCode != 0 ]]
  then
    log "Failed too many times! ($@)"
  fi

  set -e

  return $exitCode
}

trap cleanup 0

set -e

full_run
`
