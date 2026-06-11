import time
import warnings
from asyncio import wait_for
from http import HTTPStatus
from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException
from fastapi.responses import RedirectResponse
from playwright_captcha import CaptchaType

from src.consts import CHALLENGE_TITLES
from src.models import (
    HealthcheckResponse,
    LinkRequest,
    LinkResponse,
    Solution,
)
from src.utils import CamoufoxDepClass, TimeoutTimer, get_camoufox, logger

warnings.filterwarnings("ignore", category=SyntaxWarning)


router = APIRouter()

CamoufoxDep = Annotated[CamoufoxDepClass, Depends(get_camoufox)]


@router.get("/", include_in_schema=False)
def read_root():
    """Redirect to /docs."""
    logger.debug("Redirecting to /docs")
    return RedirectResponse(url="/docs", status_code=301)


@router.get("/health")
async def health_check(sb: CamoufoxDep):
    """Health check endpoint."""
    health_check_request = await read_item(
        LinkRequest.model_construct(url="https://google.com"),
        sb,
    )

    if health_check_request.solution.status != HTTPStatus.OK:
        raise HTTPException(
            status_code=500,
            detail="Health check failed",
        )

    return HealthcheckResponse(user_agent=health_check_request.solution.user_agent)


@router.post("/v1")
async def read_item(request: LinkRequest, dep: CamoufoxDep) -> LinkResponse:
    """Handle POST requests."""
    start_time = int(time.time() * 1000)

    timer = TimeoutTimer(duration=request.max_timeout)

    request.url = request.url.replace('"', "").strip()
    page_request = None
    status = HTTPStatus.OK
    download_event = None

    def on_download(download):
        nonlocal download_event
        download_event = download

    dep.page.on("download", on_download)

    try:
        page_request = await dep.page.goto(
            request.url, timeout=timer.remaining() * 1000
        )
        status = page_request.status if page_request else HTTPStatus.OK
        await dep.page.wait_for_load_state(
            state="domcontentloaded", timeout=timer.remaining() * 1000
        )
        try:
            await dep.page.wait_for_load_state(
                "networkidle", timeout=min(5000, timer.remaining() * 1000)
            )
        except Exception as idle_err:
            logger.warning(f"Networkidle wait timed out or failed: {idle_err}. Proceeding with domcontentloaded state.")

        if await dep.page.title() in CHALLENGE_TITLES:
            logger.info("Challenge detected, attempting to solve...")
            # Solve the captcha
            await wait_for(
                dep.solver.solve_captcha(  # pyright: ignore[reportUnknownMemberType,reportUnknownArgumentType]
                    captcha_container=dep.page,
                    captcha_type=CaptchaType.CLOUDFLARE_INTERSTITIAL,
                    wait_checkbox_attempts=1,
                    wait_checkbox_delay=0.5,
                ),
                timeout=timer.remaining(),
            )
            status = HTTPStatus.OK
            logger.debug("Challenge solved successfully.")
    except TimeoutError as e:
        logger.error("Timed out while solving the challenge")
        raise HTTPException(
            status_code=408,
            detail="Timed out while solving the challenge",
        ) from e
    except Exception as e:
        if "download" in str(e).lower():
            logger.info("Download started, waiting to capture file...")
            import asyncio
            for _ in range(50):
                if download_event:
                    break
                await asyncio.sleep(0.1)
            
            if download_event:
                path = await download_event.path()
                with open(path, "rb") as f:
                    content = f.read()
                
                # Decode as latin-1 to keep raw 8-bit values intact inside JSON string
                response_content = content.decode("latin-1")
                status = HTTPStatus.OK
                cookies = await dep.context.cookies()
                
                return LinkResponse(
                    message="Success",
                    solution=Solution(
                        user_agent=await dep.page.evaluate("navigator.userAgent"),
                        url=dep.page.url,
                        status=status,
                        cookies=cookies,
                        headers=page_request.headers if page_request else {},
                        response=response_content,
                    ),
                    start_timestamp=start_time,
                )
            else:
                logger.error("Download started but was not captured")
                raise e
        else:
            logger.error(f"Navigation exception: {e}")
            raise e
    finally:
        try:
            dep.page.remove_listener("download", on_download)
        except Exception:
            pass

    cookies = await dep.context.cookies()

    return LinkResponse(
        message="Success",
        solution=Solution(
            user_agent=await dep.page.evaluate("navigator.userAgent"),
            url=dep.page.url,
            status=status,
            cookies=cookies,
            headers=page_request.headers if page_request else {},
            response=await dep.page.content(),
        ),
        start_timestamp=start_time,
    )
