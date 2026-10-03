import FreeCAD as App
import Part, math

doc = App.newDocument('FlangeCoupling')

# ========== Parameters ==========
OD, ID, THK = 120, 50, 20
PCD, BOLT_D = 90, 12
CB_D, CB_H = 20, 2
KEY_W, KEY_D = 14, 5.5
RIB_T = 8
FIL_IN, FIL_OUT = 2, 3
RHO = 7.85e-6
BOLT_COUNT = 6

print('Params set')

# ========== 1. Flange body ==========
outer = doc.addObject('Part::Cylinder', 'Outer')
outer.Radius = OD/2
outer.Height = THK
doc.recompute()

inner_cut = doc.addObject('Part::Cylinder', 'InnerCut')
inner_cut.Radius = ID/2
inner_cut.Height = THK

flange = doc.addObject('Part::Cut', 'FlangeRing')
flange.Base = outer
flange.Tool = inner_cut
doc.recompute()
print('Flange ring done')

# ========== 2. Bolt holes + counterbores ==========
tools = []
for i in range(BOLT_COUNT):
    angle = 2 * math.pi * i / BOLT_COUNT
    x = PCD/2 * math.cos(angle)
    y = PCD/2 * math.sin(angle)

    bh = doc.addObject('Part::Cylinder', f'BH_{i}')
    bh.Radius = BOLT_D/2
    bh.Height = THK + 2
    bh.Placement.Base = App.Vector(x, y, -1)
    tools.append(bh)

    cbt = doc.addObject('Part::Cylinder', f'CB_T_{i}')
    cbt.Radius = CB_D/2
    cbt.Height = CB_H + 0.1
    cbt.Placement.Base = App.Vector(x, y, THK - CB_H)
    tools.append(cbt)

    cbb = doc.addObject('Part::Cylinder', f'CB_B_{i}')
    cbb.Radius = CB_D/2
    cbb.Height = CB_H + 0.1
    cbb.Placement.Base = App.Vector(x, y, -0.1)
    tools.append(cbb)

tool_fuse = doc.addObject('Part::MultiFuse', 'HoleTools')
tool_fuse.Shapes = tools
doc.recompute()

flange_h = doc.addObject('Part::Cut', 'FlangeHoles')
flange_h.Base = flange
flange_h.Tool = tool_fuse
doc.recompute()
print(f'Holes done: {len(tools)} tools')

# ========== 3. Keyway ==========
key = doc.addObject('Part::Box', 'KeywayCut')
key.Length = KEY_W
key.Width = THK + 4
key.Height = KEY_D + 2
key.Placement.Base = App.Vector(-KEY_W/2, ID/2 - 1, -2)

flange_k = doc.addObject('Part::Cut', 'FlangeKeyed')
flange_k.Base = flange_h
flange_k.Tool = key
doc.recompute()
print('Keyway done')

# ========== 4. Ribs (6 pieces, offset 30 degrees from bolt holes) ==========
rib_offset = math.radians(30)
ribs = []
for i in range(6):
    angle = 2 * math.pi * i / 6 + rib_offset
    rib = doc.addObject('Part::Box', f'Rib_{i}')
    rib.Length = RIB_T
    rib.Width = (OD/2 - ID/2)
    rib.Height = 15
    rx = (ID/2 + rib.Width/2) * math.cos(angle)
    ry = (ID/2 + rib.Width/2) * math.sin(angle)
    rib.Placement.Base = App.Vector(rx - RIB_T/2 * math.cos(angle) - RIB_T/2, ry - RIB_T/2 * math.sin(angle) - RIB_T/2, 0)
    rot = App.Rotation(App.Vector(0,0,1), math.degrees(angle))
    rib.Placement.Rotation = rot
    ribs.append(rib)

rib_fuse = doc.addObject('Part::MultiFuse', 'AllRibs')
rib_fuse.Shapes = ribs
doc.recompute()

flange_r = doc.addObject('Part::Fuse', 'FlangeRibbed')
flange_r.Base = flange_k
flange_r.Tool = rib_fuse
doc.recompute()
print('Ribs done')

# ========== 5. Fillets ==========
try:
    fillet = doc.addObject('Part::Fillet', 'Fillets')
    fillet.Base = flange_r
    fillet.Radius = FIL_OUT
    doc.recompute()
    print('Fillets done')
except Exception as e:
    print(f'Fillet warning: {e}')

# Final naming
flange_r.Label = 'FlangeCoupling'

# ========== 6. Report ==========
doc.recompute()
try:
    vol = flange_r.Shape.Volume
    mass = vol * RHO
    print(f'Volume: {vol:.1f} mm^3')
    print(f'Mass: {mass:.3f} kg')
except:
    print('Could not compute volume')

print('=== DONE ===')
