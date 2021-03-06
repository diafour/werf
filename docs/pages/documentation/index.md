---
permalink: documentation/index.html
sidebar: documentation
---

{%- asset overview.css %}

<div class="overview">
    <div class="overview__title">Must-Read</div>
    <div class="overview__row">
        <div class="overview__step">
            <div class="overview__step-header">
                <div class="overview__step-num">1</div>
                <div class="overview__step-time">5 minutes</div>
            </div>
            <div class="overview__step-title">Start your journey with basics</div>
            <div class="overview__step-actions">
                <a class="overview__step-action" href="{{ "introduction.html" | relative_url }}">Introduction</a>
            </div>
        </div>
        <div class="overview__step">
            <div class="overview__step-header">
                <div class="overview__step-num">2</div>
                <div class="overview__step-time">15 minutes</div>
            </div>
            <div class="overview__step-title">Install and give werf a try on an example project</div>
            <div class="overview__step-actions">
                <a class="overview__step-action" href="{{ "installation.html" | relative_url }}">Installation</a>
                <a class="overview__step-action" href="{{ "documentation/quickstart.html" | relative_url }}">Quickstart</a>
            </div>
        </div>
    </div>
    <div class="overview__step">
        <div class="overview__step-header">
            <div class="overview__step-num">3</div>
            <div class="overview__step-time">15 minutes</div>
        </div>
        <div class="overview__step-title">Learn the essentials of using werf in any CI/CD system</div>
        <div class="overview__step-actions">
            <a class="overview__step-action" href="{{ "documentation/using_with_ci_cd_systems.html" | relative_url }}">Using werf with CI/CD systems</a>
        </div>
    </div>
    <div class="overview__step">
        <div class="overview__step-header">
            <div class="overview__step-num">4</div>
            <div class="overview__step-time">several hours</div>
        </div>
        <div class="overview__step-title">Find a guide suitable for your project</div>
        <div class="overview__step-actions">
            <a class="overview__step-action" href="{{ "documentation/guides.html" | relative_url }}">Guides</a>
        </div>
        <div class="overview__step-info">
            Find a guide suitable for your project (filter by a programming language, framework, CI/CD system, etc.) and deploy your first real application into the Kubernetes cluster with werf.
        </div>
    </div>
    <div class="overview__title">Reference</div>
    <div class="overview__step">
        <div class="overview__step-header">
            <div class="overview__step-num">1</div>
        </div>
        <div class="overview__step-title">Use Reference for structured information about werf configuration and commands</div>
        <div class="overview__step-actions">
            <a class="overview__step-action" href="{{ "documentation/reference/werf_yaml.html" | relative_url }}">Reference</a>
        </div>
        <div class="overview__step-info">
<div markdown="1">
 - An application should be properly configured via the [`werf.yaml`]({{ "documentation/reference/werf_yaml.html" | relative_url }}) file to use werf.
 - werf also uses [annotations]({{ "documentation/reference/deploy_annotations.html" | relative_url }}) in resources definitions to configure the deploy behaviour.
 - The [command line interface]({{ "documentation/reference/cli/overview.html" | relative_url }}) article contains the full list of werf commands with a description.
</div>
        </div>
    </div>
    <div class="overview__title">The extra mile</div>
    <div class="overview__step">
        <div class="overview__step-header">
            <div class="overview__step-num">1</div>
        </div>
        <div class="overview__step-title">Get the deep knowledge, which you will need eventually during werf usage</div>
        <div class="overview__step-actions">
            <a class="overview__step-action" href="{{ "documentation/advanced/configuration/supported_go_templates.html" | relative_url }}">Advanced</a>
        </div>
        <div class="overview__step-info">
<div markdown="1">
 - [Configuration]({{ "documentation/advanced/configuration/supported_go_templates.html" | relative_url }}) informs about templating principles of werf configuration files as well as generating deployment-related names (such as a Kubernetes namespace or a release name).
 - [Helm]({{ "documentation/advanced/helm/basics.html" | relative_url }})** describes the deploy essentials: how to configure werf for deploying to Kubernetes, what helm chart and release is. Here you may find the basics of templating Kubernetes resources, algorithms for using built images defined in your `werf.yaml` file during the deploy process and working with secrets, plus other useful stuff. Read this section if you want to learn more about organizing the deploy process with werf.
 - [Cleanup]({{ "documentation/advanced/cleanup.html" | relative_url }}) explains werf cleanup concepts and main commands to perform cleaning tasks.
 - [CI/CD]({{ "documentation/advanced/ci_cd/ci_cd_workflow_basics.html" | relative_url }}) describes main aspects of organizing CI/CD workflows with werf. Here you will learn how to use werf with GitLab CI/CD, GitHub Actions, or any other CI/CD system.
 - [Building images with stapel]({{ "documentation/reference/werf_yaml.html#image-section" | relative_url }}) introduces werf's custom builder. It currently implements the distributed building algorithm to enable lightning-fast build pipelines with distributed caching and incremental rebuilds based on the Git history of your application.
 - [Development and debug]({{ "documentation/advanced/development_and_debug/stage_introspection.html" | relative_url }}) describes debugging build and deploy processes of your application when something goes wrong and prvodes instructions for setting up a local development environment.
 - [Supported registry implementations]({{ "documentation/advanced/supported_registry_implementations.html" | relative_url }}) contains general info about supported implementations and authorization when using different implementations.
</div>
        </div>
    </div>
    <div class="overview__step">
        <div class="overview__step-header">
            <div class="overview__step-num">2</div>
        </div>
        <div class="overview__step-title">Dive into overview of werf's inner workings</div>
        <div class="overview__step-actions">
            <a class="overview__step-action" href="{{ "documentation/internals/building_of_images/build_process.html" | relative_url }}">Internals</a>
        </div>
        <div class="overview__step-info">
            <p>You do not have to read through this section to make full use of werf. However, those interested in werf's internal mechanics will find some valuable info here.</p>
<div markdown="1">
 - [Building images]({{ "documentation/internals/building_of_images/build_process.html" | relative_url }}) — what image builder and stages are, how stages storage works, what is the syncrhonization server, other info related to the building process.
 - [How does the CI/CD integration work?]({{ "documentation/internals/how_ci_cd_integration_works/general_overview.html" | relative_url }}).
 - [The slug algorithm for naming]({{ "documentation/internals/names_slug_algorithm.html" | relative_url }}) describes the algorithm that werf uses under-the-hood to automatically replace invalid characters in input names so that other systems (such as Kubernetes namespaces or release names) can consume them.
 - [Integration with SSH agent]({{ "documentation/internals/integration_with_ssh_agent.html" | relative_url }}) shows how to integrate ssh-keys with the building process in werf.
 - [Development]({{ "documentation/internals/development/stapel_image.html" | relative_url }}) — this developers zone contains service/maintenance manuals and other docs written by werf developers and for werf developers. All this information sheds light on how specific werf subsystems work, describes how to keep the subsystem current, how to write and build new code for the werf, etc.
</div>
        </div>
    </div>
</div>
