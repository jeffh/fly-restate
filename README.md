# Fly Restate

An example of deploying a [Restate][restate] cluster on [Fly.io][fly].

This project is based off of [fly-apps/nats-cluster][nats-cluster].

[restate]: https://restate.dev/
[fly]: https://fly.io/
[nats-cluster]: https://github.com/fly-apps/nats-cluster/

## Notes

 - This is intended to run as a regional cluster (and not a global cluster).
 - This relies on fly's encrypted IPv6 network for communication between nodes.
 - It hasn't been tested with scaling down nodes, but you should remember to
   remove the volume mounts since its not done automatically (unless you
   destroy the fly app).
 - No support here, this is a demonstration to show how to run Restate on
   Fly.io, I'm not going to maintain running your instance of this project.
 - Deploy are not verified stable/safe right now. Restate doesn't support
   dynamic reloading of configuration without restarts.

## Setup

Clone this repository follow these steps:

1. Copy `fly.toml.example` to `fly.toml`.

2. `fly launch --no-deploy --flycast`. Accept the adjustments the cli will make to the configuration.

   > This will create a new fly app that isn't publicly accessible without
   > deploying it, since we're going to modify the fly.toml file as appropriate.

3. Edit `fly.toml` environment variables to your liking. See [Restate documentation][restate-docs] for more information.

   > The `FLY_REGION` variable is set to the region you want to deploy to. This
   > is important as it will determine the region of the cluster. Also adjust
   > the mounts for the data storage volume as appropriate.

   [restate-docs]: https://docs.restate.dev/operate/configuration/server

4. If you're using snapshots, make sure to set your AWS credentials using fly secrets. Run:

   ```bash
   fly secrets set --stage RESTATE_WORKER__SNAPSHOTS__AWS_ACCESS_KEY_ID=<your-access-key-id>
   fly secrets set --stage RESTATE_WORKER__SNAPSHOTS__AWS_SECRET_ACCESS_KEY=<your-key-access-key>
   ```

   > The `--stage` flag is used to prevent fly from deploying the application immediately after.

5. Deploy the application using `fly deploy`.

6. `fly scale count 3` to set the number of nodes in the cluster.

   > This will create 3 nodes in the cluster. You can change this to any number
   > you want, but it must align with your configuration. You need a minimum of
   > 2*RESTATE_DEFAULT_REPLICATION + 1 nodes alive to be available.

7. `fly ssh console`

   > Connect to a machine to provision the cluster. This is one time per new cluster (e.g. not when adding nodes)

8. On a restate node, run `restatectl provision` to provision the cluster.

   > This will initialize the cluster. Future nodes will automatically join the provisioned cluster.

9. Verify everything is good by running `restatectl status` on the node you're
   connected to. Feel free to close the ssh session after this.

10. If you're [connected via wireguard to fly][wg], you can access the admin 
   interface at `http://<APP>.flycast`. Note that this is http only since it
   is routed through your encrypted wireguard tunnel.

[wg]: https://fly.io/docs/blueprints/connect-private-network-wireguard/
