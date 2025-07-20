# /// script
# dependencies = [
#     "flask",
#     "pymupdf",
# ]
# ///
from flask import Flask, request, jsonify
import json
import os
import pymupdf
from typing import Dict, Any
import subprocess
import platform
import shutil

def analyze_pdf_to_json(pdf_path: str) -> Dict[str, Any]:
    """
    Analyze a PDF file and return a JSON summary of its contents.

    Args:
        pdf_path: Path to the PDF file

    Returns:
        Dict containing a structured summary of the PDF contents
    """
    doc = pymupdf.open(pdf_path)

    result = {
        "filename": os.path.basename(pdf_path),
        "total_pages": len(doc),
        "pages": []
    }

    for page_num, page in enumerate(doc):
        page_data = {
            "page_number": page_num + 1,
            "images": analyze_image_data(doc, page),
        }

        result["pages"].append(page_data)

    doc.close()
    return result

def analyze_image_data(doc: pymupdf.Document, page: pymupdf.Page) -> Dict[str, Any]:
    """Extract image-related data from a page"""

    image_list = page.get_images(full=True)
    seen_images = set()
    image_details = []

    # Get page dimensions in points
    page_rect = page.rect
    page_width = page_rect.width
    page_height = page_rect.height

    # Get page rotation
    page_rotation = page.rotation
    # If rotated by 90 or 270 degrees, swap width and height for comparison
    if page_rotation in (90, 270):
        page_width, page_height = page_height, page_width

    for img_info in image_list:
        xref = img_info[0]

        # Skip if duplicate
        if xref in seen_images:
            continue

        seen_images.add(xref)

        # Extract image data
        base_image = doc.extract_image(xref)
        if not base_image:
            continue

        image_bytes = base_image["image"]
        width = base_image.get("width", 0)
        height = base_image.get("height", 0)
        colorspace = base_image.get("colorspace", 0)

        # Convert colorspace number to description
        cs_name = "Unknown"
        if colorspace == 1:
            cs_name = "Gray"
        elif colorspace == 3:
            cs_name = "RGB"
        elif colorspace == 4:
            cs_name = "CMYK"

        # Determine if image is full page
        # Heuristic: Image covers more than 75% of page area and maintains similar aspect ratio
        image_aspect_ratio = width / height if height else 0
        page_aspect_ratio = page_width / page_height if page_height else 0

        # Calculate area coverage (comparing image to page)
        # We use DPI of 72 (default PDF point resolution) to compare dimensions
        img_area_in_points = (width / 72 * 72) * (height / 72 * 72)  # simplified but keeps the concept
        page_area = page_width * page_height
        area_coverage = img_area_in_points / page_area if page_area else 0

        # Aspect ratio difference (0 = identical, higher = more different)
        aspect_difference = abs(image_aspect_ratio - page_aspect_ratio) / page_aspect_ratio if page_aspect_ratio else 999

        # Apply heuristic - image is full page if it covers sufficient area and has similar aspect ratio
        # is_full_page = area_coverage > 0.75 and aspect_difference < 0.25
        is_full_page = area_coverage > 0.60 and aspect_difference < 0.35

        # print(area_coverage, aspect_difference)

        image_details.append({
            "width": width,
            "height": height,
            "colorspace": cs_name,
            "size_bytes": len(image_bytes),
            "is_full_page": is_full_page,
            "coverage_ratio": round(area_coverage, 2),
            "dpi_estimate": round(width / page_width * 72) if page_width else 0  # Rough DPI estimate
        })

    return {
        "count": len(image_details),
        "details": image_details
    }


def pdf_analysis_api(pdf_path: str, pretty: bool = True) -> str:
    """
    API function to analyze a PDF file and return a JSON summary.

    Args:
        pdf_path: Path to the PDF file
        pretty: Whether to format the JSON with indentation (default: True)

    Returns:
        JSON string containing the analysis results
    """
    try:
        result = analyze_pdf_to_json(pdf_path)
        indent = 2 if pretty else None
        json_output = json.dumps(result, indent=indent)

        return json_output

    except Exception as e:
        error_result = {
            "error": True,
            "message": str(e),
            "filename": os.path.basename(pdf_path) if pdf_path else "unknown"
        }
        return json.dumps(error_result, indent=2 if pretty else None)

if __name__ == '__main__':
    app = Flask(__name__)

    @app.route('/analyze', methods=['POST'])
    def analyze():
        data = request.json
        file_path = data.get('file_path')

        if not file_path or not os.path.exists(file_path):
            return jsonify({"error": "File not found: " + file_path})

        try:
            return pdf_analysis_api(file_path)
        except Exception as e:
            return jsonify({"error": str(e)})

    @app.route('/extract', methods=['POST'])
    def extract_text():
        data = request.json
        file_path = data.get('file_path')

        if not file_path or not os.path.exists(file_path):
            return jsonify({"error": "File not found: " + file_path})

        file_dir = os.path.dirname(file_path)
        file_name = os.path.basename(file_path)
        file_name_without_ext = os.path.splitext(file_name)[0]

        output_txt_path = os.path.join(file_dir, file_name_without_ext + ".txt")

        try:
            doc = pymupdf.open(file_path)
            text = ""
            for page in doc:
                text += page.get_text()

            with open(output_txt_path, 'w') as output_file:
                output_file.write(text)

            return jsonify({"wrote_file": output_txt_path})

        except Exception as _:
            pass # pymupdf failed so fall back to libreoffice

        try:
            if platform.system() == "Darwin":  # macOS
                libreoffice_paths = [
                    "/Applications/LibreOffice.app/Contents/MacOS/soffice",
                    "/Applications/LibreOffice.app/Contents/Resources/soffice"
                ]
            elif platform.system() == "Linux":
                # On Debian/Ubuntu systems, the binary is usually at /usr/bin/soffice
                # Also check other common locations
                libreoffice_paths = [
                    "/usr/bin/soffice",
                    "/usr/bin/libreoffice",
                    "/usr/lib/libreoffice/program/soffice",
                    "/opt/libreoffice/program/soffice"
                ]
            else:  # Windows or other systems
                libreoffice_paths = ["soffice"]  # Assume it's in PATH

            # Try to find the first available LibreOffice binary
            libreoffice_path = None
            for path in libreoffice_paths:
                if os.path.exists(path):
                    libreoffice_path = path
                    break

            # If not found in predefined paths, try to find in PATH
            if libreoffice_path is None:
                libreoffice_path = shutil.which("soffice") or shutil.which("libreoffice")

            if libreoffice_path is None:
                raise Exception("LibreOffice not found. Please install LibreOffice or specify its path.")

            command = [
                libreoffice_path,
                "--convert-to", "txt:Text (encoded):UTF8",
                "--headless",
                "--outdir", file_dir,
                file_path
            ]

            print(command)

            result = subprocess.run(command, capture_output=True, text=True)

            print(result)

            if result.returncode != 0:
                return jsonify({"error": f"LibreOffice conversion failed: {result.stderr}"})
            else:
                return jsonify({"wrote_file": output_txt_path})

        except Exception as e:
            return jsonify({"error": str(e)})

    app.run(host="0.0.0.0", port=11000)
