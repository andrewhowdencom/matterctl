# Share an existing Matter light

Use this procedure for a Matter light already working in Google Home. The
device must support Matter directly or through a Matter bridge and be reachable
from the Matter Server host over its existing IP network. Merely appearing in
Google Home does not establish Matter support.

## From the browser

1. Open matterctl's device page on the home LAN. Wait until the connection shows
   **Live**, then select **Add device**.
2. In Google Home, open the selected device's settings and its Matter sharing
   option (often **Linked Matter apps & services**). Obtain a fresh pairing code
   while the sharing window is open. Leave the device on its existing network.
3. Paste or type the manual code, or paste an `MT:` QR payload. Optionally enter
   a device name, then select **Add device**. Keep the page open while Matter
   Server adds the device and confirms its label.
4. On success, the page shows the new node ID and refreshes the device list.
   Check that the device is present and remains usable in Google Home.

If you select **Stop waiting**, close the page, lose the connection, or reach a
timeout, the result may be unknown. Matter Server may still finish adding the
device. Check the device list and the device itself before trying again. The
browser never retries automatically. The web flow does not use the camera or
provision a fresh device over Bluetooth, Wi-Fi, or Thread.

## From the CLI

1. Check `./bin/matterctl server info` and `./bin/matterctl device list`.
2. In Google Home, open the selected device's settings and its Matter sharing
   option (often **Linked Matter apps & services**). Open a sharing window and
   obtain a fresh pairing code. Leave the device on its existing network.
3. Optionally run `./bin/matterctl device discover`. It lists network devices
   advertising an open commissioning window. An empty scan is not a complete
   test of reachability; commissioning also searches for the supplied code.
4. Run the following in Bash on the host, paste the fresh sharing code when
   prompted, and press Enter. Hidden input and stdin keep the code out of shell
   history and process arguments. The code is discarded from the shell afterward.

   ```bash
   read -r -s -p 'Fresh sharing code: ' matterctl_pairing_code
   printf '\n'
   printf '%s\n' "$matterctl_pairing_code" | ./bin/matterctl device commission --code-stdin --network-only
   unset matterctl_pairing_code
   ```

   The CLI asks for an optional device name on a terminal before commissioning.
   Press Enter to skip it, or use `--name "Den Light"` in a script. The CLI
   reports that it is waiting, then prints the commissioned device and
   its endpoint capabilities when Matter Server confirms completion. The default
   wait is five minutes; use `--timeout 7m` if needed. `--output json` prints the
   device as JSON on stdout, with waiting/completion messages on stderr. JSON
   mode does not prompt; use `--name` to set a name in that mode.
5. Run `./bin/matterctl device list`, then `./bin/matterctl device inspect <node>`
   using the returned node ID. Look for `power.on` and `power.off` on an endpoint.
   A light device type alone does not guarantee those commands are advertised.
6. Verify the light remains usable in Google Home. Sharing adds Matter Server
   as another controller; this workflow does not request removal from Google Home.

Matter labels may differ from Google Home's names. If you skipped the name or
the device rejected the write, set the node's Matter label later, then use that
name for control:

```sh
./bin/matterctl device label set <node-id> "Den Light"
./bin/matterctl device power on "Den Light"
./bin/matterctl device power off "Den Light"
```

Commissioning can succeed while the later label write fails. In that case,
matterctl reports the new node ID and the naming failure. Use that node ID to
retry `device label set`; do not repeat commissioning to fix a name.

Numeric node and endpoint IDs also work:

```sh
./bin/matterctl device power on <node-id> --endpoint <endpoint-id>
./bin/matterctl device power off <node-id> --endpoint <endpoint-id>
```

The command returns success only after a fresh post-command read confirms the
reported state. A timeout or connection loss can leave the physical outcome
unknown; inspect the device before deciding whether to send another command.

If the attempt fails, use the guidance accompanying the upstream error. A
missing device generally calls for a fresh sharing window and a reachability
check; a rejected code calls for a fresh code. Ctrl-C, a deadline, or a connection
loss stops waiting and can leave the outcome unknown. Matter Server may continue
commissioning. Check inventory and allow the existing attempt to finish before
starting another one; matterctl never automatically replays commissioning.

Keep Matter Server's API at `127.0.0.1:5580/ws`. No Bluetooth commissioning,
network credential changes, factory reset, or host networking changes are part
of this procedure. The protocol for network-only commissioning is described in
the [Matter Server v1.5.5 API](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/websockets_api.md#commissioning).
