# BILRESA's third group controls Den Light endpoint 1.
# Endpoint 9 is the wheel press; endpoints 7 and 8 are the wheel directions.
den = device("Den Light", endpoint=1)
bilresa = device("2")

on(short_release(bilresa, endpoint=9), lambda event: [toggle(den)])
on(wheel_step(bilresa, up=7, down=8),
   lambda event: [adjust_brightness(den, event.steps * 5)])
