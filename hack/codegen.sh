CUSTOM_RESOURCE_NAME="samplecontroller"
CUSTOM_RESOURCE_VERSION="v1alpha1"

ROOT_PACKAGE=$(cd "$(dirname BASH_SOURCE[0])" && pwd)
CODEGEN_PKG=$1

#echo ${ROOT_PACKAGE} ${CODEGEN_PKG}

if [ $# -lt 1 ]; then
    echo "need code-generator path as parameter"
    exit 0
fi

#
#source "${CODEGEN_PKG}/kube_codegen.sh"
#
#kube::codegen::gen_helpers \
#    --input-pkg-root "${ROOT_PACKAGE}/pkg/apis" \
#    --output-base "${ROOT_PACKAGE}"
##    --boilerplate "${ROOT_PACKAGE}/hack/boilerplate.go.txt"
#
#kube::codegen::gen_client \
#    --with-watch \
#    --input-pkg-root "${ROOT_PACKAGE}/pkg/apis" \
#    --output-base "${ROOT_PACKAGE}" \
#    --output-pkg-root "${ROOT_PACKAGE}/pkg/generated"
##    --boilerplate "${ROOT_PACKAGE}/hack/boilerplate.go.txt"

bash "${CODEGEN_PKG}"/generate-groups.sh "deepcopy,client,informer,lister" \
  "sample-controller/pkg/generated" \
  "sample-controller/pkg/apis" \
  "${CUSTOM_RESOURCE_NAME}:${CUSTOM_RESOURCE_VERSION}" \
  --output-base "${ROOT_PACKAGE}/.." \
  --go-header-file "${ROOT_PACKAGE}/hack/boilerplate.go.txt"