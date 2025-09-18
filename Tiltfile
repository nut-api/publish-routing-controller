# -*- mode: Python -*-
load("ext://uibutton", "cmd_button", "location")
load("ext://restart_process", "docker_build_with_restart")

# kustomize_cmd = "./hack/tools/bin/kustomize"
# envsubst_cmd = "./hack/tools/bin/envsubst"
# tools_bin = "./hack/tools/bin"

#Add tools to path
# os.putenv("PATH", os.getenv("PATH") + ":" + tools_bin)

update_settings(k8s_upsert_timeout_secs = 60)  # on first tilt up, often can take longer than 30 seconds

# # set defaults
# settings = {
#     "allowed_contexts": [
#         "kind-webrenderer",
#     ],
#     "deploy_cert_manager": True,
#     "deploy_observability": False,
#     "preload_images_for_kind": True,
#     "kind_cluster_name": "webrenderer",
#     "capi_version": "v1.8.10",
#     "cabpt_version": "v0.5.6",
#     "cacppt_version": "v0.4.11",
#     "cert_manager_version": "v1.11.0",
#     "extra_args": {
#         "hetzner": [
#             "--log-level=debug",
#         ],
#     },
#     "kustomize_substitutions": {
#     },
# }

# # global settings
# settings.update(read_yaml(
#     "tilt-settings.yaml",
#     default = {},
# ))

# if settings.get("trigger_mode") == "manual":
#     trigger_mode(TRIGGER_MODE_MANUAL)

# if "allowed_contexts" in settings:
#     allow_k8s_contexts(settings.get("allowed_contexts"))

# if "default_registry" in settings:
#     default_registry(settings.get("default_registry"))

# # deploy CAPI
# def deploy_capi():
#     version = settings.get("capi_version")
#     capi_uri = "https://github.com/kubernetes-sigs/cluster-api/releases/download/{}/cluster-api-components.yaml".format(version)
#     cmd = "curl -sSL {} | {} | kubectl apply -f -".format(capi_uri, envsubst_cmd)
#     local(cmd, quiet = True)
#     if settings.get("extra_args"):
#         extra_args = settings.get("extra_args")
#         if extra_args.get("core"):
#             core_extra_args = extra_args.get("core")
#             if core_extra_args:
#                 for namespace in ["capi-system", "capi-webhook-system"]:
#                     patch_args_with_extra_args(namespace, "capi-controller-manager", core_extra_args)
#         if extra_args.get("kubeadm-bootstrap"):
#             kb_extra_args = extra_args.get("kubeadm-bootstrap")
#             if kb_extra_args:
#                 patch_args_with_extra_args("capi-kubeadm-bootstrap-system", "capi-kubeadm-bootstrap-controller-manager", kb_extra_args)


# def patch_args_with_extra_args(namespace, name, extra_args):
#     args_str = str(local("kubectl get deployments {} -n {} -o jsonpath='{{.spec.template.spec.containers[0].args}}'".format(name, namespace)))
#     args_to_add = [arg for arg in extra_args if arg not in args_str]
#     if args_to_add:
#         args = args_str[1:-1].split()
#         args.extend(args_to_add)
#         patch = [{
#             "op": "replace",
#             "path": "/spec/template/spec/containers/0/args",
#             "value": args,
#         }]
#         local("kubectl patch deployment {} -n {} --type json -p='{}'".format(name, namespace, str(encode_json(patch)).replace("\n", "")))

# # Users may define their own Tilt customizations in tilt.d. This directory is excluded from git and these files will
# # not be checked in to version control.
# def include_user_tilt_files():
#     user_tiltfiles = listdir("tilt.d")
#     for f in user_tiltfiles:
#         include(f)

# def append_arg_for_container_in_deployment(yaml_stream, name, namespace, contains_image_name, args):
#     for item in yaml_stream:
#         if item["kind"] == "Deployment" and item.get("metadata").get("name") == name and item.get("metadata").get("namespace") == namespace:
#             containers = item.get("spec").get("template").get("spec").get("containers")
#             for container in containers:
#                 if contains_image_name in container.get("name"):
#                     container.get("args").extend(args)

# def fixup_yaml_empty_arrays(yaml_str):
#     yaml_str = yaml_str.replace("conditions: null", "conditions: []")
#     return yaml_str.replace("storedVersions: null", "storedVersions: []")

# def set_env_variables():
#     substitutions = settings.get("kustomize_substitutions", {})
#     print(substitutions)
#     arr = [(key, val) for key, val in substitutions.items()]
#     for key, val in arr:
#         os.putenv(key, val)

## This should have the same versions as the Dockerfile
tilt_dockerfile_header = """
FROM gcr.io/distroless/base:debug as tilt
WORKDIR /

COPY bin/manager .
"""

# Build webrenderer and add feature gates
def webrenderer():
    # Set up a local_resource build of the provider's manager binary.

    # Forge the build command
    build_env = "CGO_ENABLED=0 GOOS=linux GOARCH=arm64"
    build_cmd = "{build_env} go build -o bin/manager cmd/main.go".format(
        build_env = build_env,
    )
    local_resource(
        "manager",
        cmd = build_cmd,
        deps = ["internal", "cmd", "config", "pkg", "go.mod", "go.sum"],
        labels = ["webrenderer"],
    )

    entrypoint = ["/manager"]


    # Set up an image build for the provider. The live update configuration syncs the output from the local_resource
    # build into the container.
    docker_build_with_restart(
        ref = "controller:latest",
        context = ".",
        dockerfile_contents = tilt_dockerfile_header,
        target = "tilt",
        only = ["bin/manager"],
        entrypoint = entrypoint,
        live_update = [
            sync("./bin/manager", "/manager"),
        ],
        ignore = ["templates"],
    )

    k8s_yaml(kustomize("config/default"))
    k8s_resource(workload = "publish-routing-controller-controller-manager", labels = ["webrenderer"])
    k8s_resource(
        objects = [
            "publish-routing-controller-system:namespace",
            "publish-routing-controller-controller-manager:serviceaccount",
            "publish-routing-controller-leader-election-role:role",
            "publish-routing-controller-manager-role:clusterrole",
            "publish-routing-controller-metrics-auth-role:clusterrole",
            "publish-routing-controller-metrics-reader:clusterrole",
            "publish-routing-controller-leader-election-rolebinding:rolebinding",
            "publish-routing-controller-manager-rolebinding:clusterrolebinding",
            "publish-routing-controller-metrics-auth-rolebinding:clusterrolebinding"
        ],
        new_name = "webrenderer-misc",
        labels = ["webrenderer"],
    )

def base64_encode(to_encode):
    encode_blob = local("echo '{}' | tr -d '\n' | base64 - | tr -d '\n'".format(to_encode), quiet = True)
    return str(encode_blob)

def base64_encode_file(path_to_encode):
    encode_blob = local("cat {} | tr -d '\n' | base64 - | tr -d '\n'".format(path_to_encode), quiet = True)
    return str(encode_blob)

def read_file_from_path(path_to_read):
    str_blob = local("cat {} | tr -d '\n'".format(path_to_read), quiet = True)
    return str(str_blob)

def base64_decode(to_decode):
    decode_blob = local("echo '{}' | base64 --decode -".format(to_decode), quiet = True)
    return str(decode_blob)

def waitforsystem():
    local("kubectl wait --for=condition=ready --timeout=300s pod --all -n publish-routing-controller-system")


##############################
# Actual work happens here
##############################
# ensure_envsubst()
# ensure_kustomize()

# include_user_tilt_files()

# set_env_variables()

webrenderer()

# waitforsystem()
