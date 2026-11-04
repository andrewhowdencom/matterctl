# Run SkyConnect on an existing Thread network

This host uses a Nabu Casa SkyConnect as an OpenThread Border Router (OTBR).
The radio runs OpenThread RCP firmware. OTBR joins the existing Google Thread
network; it must not create a second network.

The checked-in deployment is specific to `boxhagener76`: Wi-Fi backbone `wlo1`,
the SkyConnect serial number in `deploy/otbr.container`, and ports 8082 and 8083.
Review those values before using it on another host.

## Prepare the host

1. Flash SkyConnect with the [Nabu Casa OpenThread RCP firmware](https://github.com/NabuCasa/silabs-firmware-builder/releases).
   The installed radio uses 460800 baud. Use the
   [Universal Silicon Labs Flasher](https://github.com/NabuCasa/universal-silabs-flasher)
   to probe the radio and confirm it reports `SL-OPENTHREAD`.
2. Pull the [OpenThread Border Router image](https://openthread.io/guides/border-router/build-docker)
   into the rootful Podman store:

   ```sh
   sudo podman pull docker.io/openthread/border-router:stable
   ```

3. Verify IPv4 and IPv6 forwarding are enabled on the host. Install the
   interface-specific router advertisement settings and the Quadlet:

   ```sh
   sudo install -D -m 0644 deploy/otbr-sysctl.conf /etc/sysctl.d/70-otbr-skyconnect.conf
   sudo /usr/sbin/sysctl -p /etc/sysctl.d/70-otbr-skyconnect.conf
   sudo install -d -m 0700 /var/lib/otbr
   sudo install -D -m 0644 deploy/otbr.container /etc/containers/systemd/otbr.container
   sudo systemctl daemon-reload
   sudo systemctl start otbr.service
   ```

   The Quadlet uses host networking, `NET_ADMIN`, `NET_RAW`, `/dev/net/tun`, and
   the SkyConnect serial port. Its REST and web listeners bind to localhost.
   The `[Install]` section activates the generated service at boot.

4. Check the service and radio:

   ```sh
   sudo systemctl is-active otbr.service
   sudo podman exec otbr ot-ctl state
   curl http://127.0.0.1:8082/node/state
   ```

   Before importing credentials, `disabled` is expected. The service does not
   form a Thread network automatically.

## Join the existing network

Obtain the active operational dataset from an Android phone signed into the
Google Home that owns the network. Google's [Thread Network API](https://developers.home.google.com/thread)
requires an app and an on-phone consent prompt. Google's
[sample app](https://developers.home.google.com/codelabs/thread-sharing-apis)
exposes **Developer Utilities → Thread Network → GPS Preferred Credentials → Get**.
Check its network name and extended PAN ID against the intended Google border
router before importing. On this host the target is `NEST-PAN-3269`, extended PAN
ID `3AD901A275C9DFFC`.

The dataset contains the Thread network key. Keep it out of the repository,
shell history, and command arguments. With OTBR disabled and no active dataset,
enter the dataset TLV as one uninterrupted hexadecimal string:

```bash
read -r -s -p 'Thread dataset TLV: ' thread_dataset
printf '\n'
printf '%s' "$thread_dataset" | curl --fail --silent --show-error \
  --request PUT --header 'Content-Type: text/plain' \
  --header 'If-None-Match: *' --data-binary @- \
  http://127.0.0.1:8082/node/dataset/active
unset thread_dataset
curl --fail --silent --show-error --request PUT \
  --header 'Content-Type: application/json' --data '"enable"' \
  http://127.0.0.1:8082/node/state
```

Then check `ot-ctl state` becomes `router` or `child`, and confirm the advertised
network name, extended PAN ID, and partition ID match the Google routers. The
[OpenThread REST API](https://github.com/openthread/ot-br-posix/blob/main/src/rest/openapi.yaml)
permits writing an active dataset only while Thread is inactive.

Matter Server also needs this dataset for first-time Bluetooth commissioning of
Matter-over-Thread devices. Store it with Matter Server's
[`set_thread_dataset` command](https://github.com/matter-js/matterjs-server/blob/v1.5.5/docs/websockets_api.md#credentials)
and confirm `matterctl server info` reports `thread_credentials_set: true`.
The current matterctl browser and CLI only perform network-only commissioning;
they cannot provision a fresh Thread device over Bluetooth. Matter Server itself
supports Bluetooth commissioning and opening a multi-admin sharing window.

Never commit the dataset or a QR code containing it.
