package templates

// Rename dummy_template to DummyTemplate to export it
// const RayActorTemplate = `
// import ray
// import time
// import sys
// import os
// import importlib

// @ray.remote(max_restarts={{.MaxActorRestarts}}, max_task_retries={{.MaxTaskRetries}})
// class {{.Actor}}:
//     def {{.Runner}}(self):
//         try:
//             directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")

//             # Get the file name without the extension
//             file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

//             sys.path.append(directory_path)

//             # Dynamically import the module
//             pipeline_module = importlib.import_module(file_name)

//             # Execute the pipeline function
//             getattr(pipeline_module, \"{{.PipelineRunner}}\")()
//         except Exception as e:
//             print(e)
//         finally:
//             ray.actor.exit_actor()

// # Initialize connection to the Ray head node on the default port.
// ray.init(address=\"auto\", namespace=\"{{.Namespace}}\")

// pipeline_runner = {{.Actor}}.options(name=\"{{.Actor}}\", lifetime=\"detached\", max_concurrency=2, num_cpus={{.NumCPUs}}).remote()
// `

const RemoteRunnerTemplate = `
import ray
import os
import sys
import importlib

ray.init(address=\"auto\", namespace=\"{{.Namespace}}\" , num_cpus=\"{{.NumCPUs}}\")
task_ref = None

# Signal handler function to cancel the Ray task
def signal_handler(signum, frame):
    global task_ref
    print(f\"Signal {signum} received. Attempting to cancel the task...\")
    if task_ref:
        ray.cancel(task_ref, force=True)  # Cancel the remote task
        print(\"Task canceled.\")
    sys.exit(0)  # Exit the program gracefully


async def f():
    directory_path = os.path.dirname(\"{{.PipelineFilePath}}\")
    file_name = os.path.splitext(os.path.basename(\"{{.PipelineFilePath}}\"))[0]

    sys.path.append(directory_path)

    # Dynamically import the pipeline module
    pipeline_module = importlib.import_module(file_name)

    # Execute the pipeline function directly
    getattr(pipeline_module, \"{{.PipelineRunner}}\")()


@ray.remote
def wrapper():
    loop = asyncio.new_event_loop()  # Create a new event loop for the worker
    asyncio.set_event_loop(loop)
    loop.run_until_complete(f())


if __name__ == \"__main__\":
    # Register signal handlers in the main thread
    signal.signal(signal.SIGINT, signal_handler)  # Handle Ctrl+C
    signal.signal(signal.SIGTERM, signal_handler)  # Handle termination signals

    # Start the remote task
    task_ref = wrapper.remote()
    print(\"Task started. Waiting for signals. Press Ctrl+C to stop.\")

    # Keep the main process running to listen for signals
    try:
        while True:
            pass
    except KeyboardInterrupt:
        signal_handler(signal.SIGINT, None)

`
